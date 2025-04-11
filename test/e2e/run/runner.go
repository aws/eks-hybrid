package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/cluster"
)

const (
	cleanupBuffer       = time.Minute * 5
	defaultTestTimeout  = time.Hour * 24
	ginkgoCleanupBuffer = time.Minute * 1
)

type E2ERunner struct {
	AwsCfg          aws.Config
	Logger          logr.Logger
	NoColor         bool
	Paths           E2EPaths
	TestLabelFilter string
	TestProcs       int
	TestTimeout     string
	TestResources   cluster.TestResources
	SkippedTests    string
}

// Run runs the E2E tests and returns the failure phase with the error
func (e *E2ERunner) Run(ctx context.Context) []Phase {
	phases := []Phase{}

	err := e.setupTestInfrastructure(ctx)
	phases = phaseCompleted(phases, phaseNameSetupTestInfrastructure, "setting up test infrastructure", err)
	if err != nil {
		e.Logger.Error(err, "Failed creating test infrastructure")
		return phases
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		// no deadline set, use 24 hours as default
		deadline = time.Now().Add(defaultTestTimeout)
	}
	ginkgoTimeout := (time.Until(deadline) - ginkgoCleanupBuffer).Round(time.Second)
	err = e.executeTests(ctx, ginkgoTimeout)
	phases = phaseCompleted(phases, phaseNameExecuteTests, "executing tests", err)
	if err != nil {
		e.Logger.Error(err, "Failed executing tests")
	}

	return phases
}

func (e *E2ERunner) setupTestInfrastructure(ctx context.Context) error {
	logger := newFileLogger(e.Paths.SetupLog, e.NoColor)
	create := cluster.NewCreate(e.AwsCfg, logger, e.TestResources.EKS.Endpoint)

	logger.Info("Creating cluster infrastructure for E2E tests...")
	if err := create.Run(ctx, e.TestResources); err != nil {
		return fmt.Errorf("creating E2E test infrastructure: %w", err)
	}

	logger.Info("E2E test infrastructure setup completed successfully!")

	return nil
}

func (e *E2ERunner) executeTests(ctx context.Context, timeout time.Duration) error {
	pwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	if timeout <= 0 {
		return fmt.Errorf("not enought time to run tests: %s", timeout)
	}

	noColorArg := ""
	if e.NoColor {
		noColorArg = "--no-color"
	}
	ginkgoArgs := []string{
		"-v",
		"-tags=e2e",
		fmt.Sprintf("--procs=%d", e.TestProcs),
		fmt.Sprintf("--skip=%s", e.SkippedTests),
		fmt.Sprintf("--label-filter=%s", e.TestLabelFilter),
		fmt.Sprintf("--output-dir=%s", e.Paths.Reports),
		fmt.Sprintf("--junit-report=%s", e2eReportsFile),
		fmt.Sprintf("--json-report=%s", e2eReportsFileJSON),
		fmt.Sprintf("--timeout=%s", timeout),
		"--fail-on-empty",
		noColorArg,
		e.Paths.TestsBinaryOrSource,
		"--",
		fmt.Sprintf("-filepath=%s", e.Paths.TestConfigFile),
	}

	outfile, err := os.Create(e.Paths.GinkgoOutputLog)
	if err != nil {
		return fmt.Errorf("creating out file: %w", err)
	}
	defer outfile.Close()

	ginkgoCmd := exec.Command(e.Paths.Ginkgo, ginkgoArgs...)
	ginkgoCmd.Dir = pwd
	ginkgoCmd.Stdout = io.MultiWriter(outfile, os.Stdout)
	ginkgoCmd.Stderr = io.MultiWriter(outfile, os.Stderr)

	e.Logger.Info(fmt.Sprintf("Running ginkgo command: %s", strings.Join(ginkgoCmd.Args, " ")))
	e.Logger.Info("-------Ginkgo command output-------")

	signalCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func(sig chan os.Signal, cmd *exec.Cmd) {
		defer signal.Stop(sig)
		for {
			select {
			case triggeredSignal := <-sig:
				if err := cmd.Process.Signal(triggeredSignal); err != nil {
					e.Logger.Error(err, "signaling ginkgo command")
				}
			case <-signalCtx.Done():
				return
			}
		}
	}(sig, ginkgoCmd)

	err = ginkgoCmd.Run()
	e.Logger.Info("-------Ginkgo command output end-------")
	if err != nil {
		return fmt.Errorf("nodeadm e2e test ginkgo command failed: %w", err)
	}

	return nil
}
