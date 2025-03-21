package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
)

type E2ERunner struct {
	AddTestPhase    func(name, message string, err error) Phase
	AwsCfg          aws.Config
	Logger          logr.Logger
	NoColor         bool
	Paths           E2EPaths
	TestLabelFilter string
	TestProcs       int
	TestTimeout     string
	TestResources   cluster.TestResources
	SkipCleanup     bool
	SkippedTests    string
}

// Run runs the E2E tests and returns the failure phase with the error
func (e *E2ERunner) Run(ctx context.Context) error {
	// After this point, return err so that the defer cleanup can combine all potential
	// errors into what is finally returned
	var err error
	defer func() {
		if e.SkipCleanup {
			e.Logger.Info("Skipping cluster and infrastructure cleanup via stack deletion")
			return
		}
		cleaner := E2ECleanup{
			AwsCfg:        e.AwsCfg,
			Logger:        e.newFileLogger(e.Paths.CleanupLog),
			TestResources: e.TestResources,
		}
		cleanupErr := cleaner.Run(ctx)
		cleanupPhase := e.AddTestPhase("cleanup-cluster", "cleaning up cluster: %w", cleanupErr)
		if cleanupErr != nil {
			err = errors.Join(err, cleanupPhase.Error)
		}
	}()

	setupErr := e.setupTestInfrastructure(ctx)
	setupPhase := e.AddTestPhase("setup-test-infrastructure", "setting up test infrastructure: %w", setupErr)
	if setupErr != nil {
		err = setupPhase.Error
		return err
	}
	testsErr := e.executeTests(ctx)
	testsPhase := e.AddTestPhase("execute-tests", "executing tests: %w", testsErr)
	if testsErr != nil {
		err = testsPhase.Error
		return err
	}
	return nil
}

func (e *E2ERunner) setupTestInfrastructure(ctx context.Context) error {
	logger := e.newFileLogger(e.Paths.SetupLog)
	create := cluster.NewCreate(e.AwsCfg, logger, e.TestResources.Endpoint)

	logger.Info("Creating cluster infrastructure for E2E tests...")
	if err := create.Run(ctx, e.TestResources); err != nil {
		return fmt.Errorf("creating E2E test infrastructure: %w", err)
	}

	logger.Info("E2E test infrastructure setup completed successfully!")

	return nil
}

func (e *E2ERunner) executeTests(ctx context.Context) error {
	pwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
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
		fmt.Sprintf("--timeout=%s", e.TestTimeout),
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

func (e *E2ERunner) newFileLogger(fileName string) logr.Logger {
	return e2e.NewLogger(e2e.LoggerConfig{NoColor: e.NoColor}, e2e.WithOutputFile(fileName))
}
