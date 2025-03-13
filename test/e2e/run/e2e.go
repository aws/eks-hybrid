package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	ginkgoTypes "github.com/onsi/ginkgo/v2/types"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/suite"
)

const (
	e2eConfigFolder      = "configs"
	e2eConfigFile        = "e2e-param.yaml"
	e2eTestResourcesFile = "e2e-test-resources.yaml"
	e2eReportsFolder     = "reports"
	e2eReportsFile       = "junit-nodeadm.xml"
	e2eReportsFileJSON   = "junit-nodeadm.json"
	serialOutputLogFile  = "serial-output.log"
	testCleanupLogFile   = "cleanup-output.log"
	testGinkgoOutputLog  = "ginkgo-output.log"
	testSetupLogFile     = "setup-output.log"
)

type E2E struct {
	AwsCfg            aws.Config
	CurrentPhase      string
	Logger            logr.Logger
	NoColor           bool
	TestConfig        suite.TestConfig
	TestLabelFilter   string
	TestsFailed       bool
	TestProcs         int
	TestTimeout       string
	TestsBinaryOrPath string
	TestResources     cluster.TestResources
	SkipCleanup       bool
	SkippedTests      string
}

type E2EResult struct {
	ArtifactsBucketPath string       `json:"artifacts_bucket_path"`
	CleanupLog          string       `json:"cleanup_log"`
	FailedTests         []FailedTest `json:"failed_tests"`
	GinkgoLog           string       `json:"ginkgo_log"`
	LastPhase           string       `json:"last_phase"`
	RunFailureMessage   string       `json:"run_failure_message"`
	SetupLog            string       `json:"setup_log"`
	TestRan             int          `json:"test_ran"`
	TotalTests          int          `json:"total_tests"`
}

type FailedTest struct {
	ArtifactsBucketPath string `json:"artifacts_bucket_path"`
	CollectorLogsBundle string `json:"collector_logs_bundle"`
	FailureMessage      string `json:"failure_message"`
	InstanceName        string `json:"instance_name"`
	GinkgoLog           string `json:"ginkgo_log"`
	Name                string `json:"name"`
	SerialLog           string `json:"serial_output_log"`
	State               string `json:"state"`
}

func (e *E2E) Run(ctx context.Context) error {
	// continue even on error to output as much as possible
	err := e.runTests(ctx)

	e.updateE2EResultPhase("parse-report")
	e2eResult, reportErr := e.parseReport()
	if reportErr != nil {
		err = errors.Join(err, fmt.Errorf("parsing report: %w", reportErr))
	}

	e.updateE2EResultPhase("upload-artifacts-to-s3")
	s3Success := true
	s3Err := e.uploadArtifactsToS3(ctx)
	if s3Err != nil {
		err = errors.Join(err, fmt.Errorf("uploading logs to s3: %w", s3Err))
		s3Success = false
	}

	e2eResult.LastPhase = e.CurrentPhase
	e2eResult = e.updateE2EResultLogs(e2eResult, s3Success)

	if err != nil {
		e2eResult.RunFailureMessage = err.Error()
	}

	if jsonErr := printE2EResultAsJSON(e2eResult); jsonErr != nil {
		err = errors.Join(err, fmt.Errorf("printing e2e result as json: %w", jsonErr))
	}

	fmt.Printf("\n")

	if textErr := e.printE2EResultAsText(e2eResult); textErr != nil {
		err = errors.Join(err, fmt.Errorf("printing e2e result as text: %w", textErr))
	}

	return err
}

func (e *E2E) runTests(ctx context.Context) error {
	e.updateE2EResultPhase("setup-directories")
	if err := e.setupDirectories(); err != nil {
		return fmt.Errorf("setting up directories: %w", err)
	}

	e.updateE2EResultPhase("write-test-configs")
	configDir := filepath.Join(e.TestConfig.ArtifactsFolder, e2eConfigFolder)
	if err := e.writeTestConfigs(configDir); err != nil {
		return fmt.Errorf("creating test config: %w", err)
	}

	var err error
	// We want to run both to ensure any dangling resources are cleaned up
	// The sweeper cleanup is configured for this specific cluster name
	defer func() {
		e.updateE2EResultPhase("cleanup-cluster")
		testCleanupLogFile := filepath.Join(e.TestConfig.ArtifactsFolder, testCleanupLogFile)
		logger := e2e.NewLogger(e2e.LoggerConfig{NoColor: e.NoColor}, e2e.WithOutputFile(testCleanupLogFile))

		var clusterErr, sweeperErr error
		if err := e.clusterStackcleanup(ctx, logger); err != nil {
			clusterErr = fmt.Errorf("running cleanup cluster via stack deletion: %w", err)
		}
		if err := e.clusterSweeperCleanup(ctx, logger); err != nil {
			sweeperErr = fmt.Errorf("running cleanup cluster via sweeper: %w", err)
		}
		if clusterErr != nil || sweeperErr != nil {
			err = errors.Join(err, clusterErr, sweeperErr)
		}
	}()

	e.updateE2EResultPhase("setup-test-infrastructure")
	if testErr := e.setupTestInfrastructure(ctx); testErr != nil {
		err = fmt.Errorf("setting up test infrastructure: %w", testErr)
		return err
	}

	e.updateE2EResultPhase("execute-tests")

	if testsErr := e.executeTests(ctx, configDir); testsErr != nil {
		err = fmt.Errorf("executing tests: %w", testsErr)
		e.TestsFailed = true
		return err
	}
	return err
}

func (e *E2E) parseReport() (E2EResult, error) {
	reportPath := filepath.Join(e.TestConfig.ArtifactsFolder, e2eReportsFolder, e2eReportsFileJSON)
	if _, err := os.Stat(reportPath); errors.Is(err, os.ErrNotExist) {
		return E2EResult{}, fmt.Errorf("reading ginkgo json report: %w", err)
	}

	e2eResult, err := e.parseJSONReport(reportPath)
	if err != nil {
		return e2eResult, fmt.Errorf("parsing ginkgo json report: %w", err)
	}

	return e2eResult, nil
}

func (e *E2E) uploadArtifactsToS3(ctx context.Context) error {
	if e.TestConfig.LogsBucket == "" {
		return nil
	}
	s3Client := s3.NewFromConfig(e.AwsCfg)
	err := filepath.WalkDir(e.TestConfig.ArtifactsFolder, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking artifacts folder: %w", err)
		}
		if info.IsDir() {
			return nil
		}

		fileRelPath := strings.Replace(path, e.TestConfig.ArtifactsFolder+"/", "", 1)
		keyPath := fmt.Sprintf("%s/%s", e.logsBucketPath(), fileRelPath)

		if err := e.uploadLogsToS3(ctx, s3Client, path, keyPath); err != nil {
			return fmt.Errorf("uploading test log to s3: %w", err)
		}

		return nil
	})

	return err
}

func (e *E2E) setupDirectories() error {
	configDir := filepath.Join(e.TestConfig.ArtifactsFolder, e2eConfigFolder)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("creating test config directory: %w", err)
	}

	testResourcesDir := filepath.Join(e.TestConfig.ArtifactsFolder, e2eReportsFolder)
	if err := os.MkdirAll(testResourcesDir, 0o755); err != nil {
		return fmt.Errorf("creating test reports directory: %w", err)
	}

	return nil
}

func (e *E2E) writeTestConfigs(configDir string) error {
	testConfigPath := filepath.Join(configDir, e2eConfigFile)

	paramsBytes, err := yaml.Marshal(e.TestConfig)
	if err != nil {
		return fmt.Errorf("marshaling params: %w", err)
	}

	if err := os.WriteFile(testConfigPath, paramsBytes, 0o644); err != nil {
		return fmt.Errorf("writing params: %w", err)
	}

	testResourcesPath := filepath.Join(configDir, e2eTestResourcesFile)

	testResourcesBytes, err := yaml.Marshal(e.TestResources)
	if err != nil {
		return fmt.Errorf("failed to marshal test resources: %v", err)
	}
	if err := os.WriteFile(testResourcesPath, testResourcesBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write test resources: %v", err)
	}
	return nil
}

func (e *E2E) setupTestInfrastructure(ctx context.Context) error {
	testSetupLogFile := filepath.Join(e.TestConfig.ArtifactsFolder, testSetupLogFile)

	logger := e2e.NewLogger(e2e.LoggerConfig{NoColor: e.NoColor}, e2e.WithOutputFile(testSetupLogFile))

	create := cluster.NewCreate(e.AwsCfg, logger, e.TestResources.Endpoint)

	logger.Info("Creating cluster infrastructure for E2E tests...")
	if err := create.Run(ctx, e.TestResources); err != nil {
		return fmt.Errorf("creating E2E test infrastructure: %w", err)
	}

	fmt.Println("E2E test infrastructure setup completed successfully!")

	return nil
}

func (e *E2E) executeTests(ctx context.Context, configDir string) error {
	ex, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	pwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	binPath := filepath.Dir(ex)
	ginkgoBinaryPath := filepath.Join(binPath, "ginkgo")

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
		fmt.Sprintf("--output-dir=%s", filepath.Join(e.TestConfig.ArtifactsFolder, e2eReportsFolder)),
		fmt.Sprintf("--junit-report=%s", e2eReportsFile),
		fmt.Sprintf("--json-report=%s", e2eReportsFileJSON),
		fmt.Sprintf("--timeout=%s", e.TestTimeout),
		"--fail-on-empty",
		noColorArg,
		e.TestsBinaryOrPath,
		"--",
		fmt.Sprintf("-filepath=%s", filepath.Join(configDir, e2eConfigFile)),
	}

	ginkgoLogFile := filepath.Join(e.TestConfig.ArtifactsFolder, testGinkgoOutputLog)
	outfile, err := os.Create(ginkgoLogFile)
	if err != nil {
		return fmt.Errorf("creating out file: %w", err)
	}
	defer outfile.Close()

	ginkgoCmd := exec.Command(ginkgoBinaryPath, ginkgoArgs...)
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

func (e *E2E) clusterStackcleanup(ctx context.Context, logger logr.Logger) error {
	if e.SkipCleanup {
		logger.Info("Skipping cluster and infrastructure cleanup via stack deletion")
		return nil
	}

	delete := cluster.NewDelete(e.AwsCfg, logger, e.TestResources.Endpoint)
	logger.Info("Cleaning up E2E cluster resources via Stack deletion")
	deleteCluster := cluster.DeleteInput{
		ClusterName:   e.TestResources.ClusterName,
		ClusterRegion: e.TestResources.ClusterRegion,
		Endpoint:      e.TestResources.Endpoint,
	}
	if err := delete.Run(ctx, deleteCluster); err != nil {
		return fmt.Errorf("cleaning up e2e resources: %w", err)
	}

	logger.Info("Cleanup completed successfully")
	return nil
}

func (e *E2E) clusterSweeperCleanup(ctx context.Context, logger logr.Logger) error {
	if e.SkipCleanup {
		logger.Info("Skipping cluster and infrastructure cleanup via sweeper")
		return nil
	}

	sweeper := cleanup.NewSweeper(e.AwsCfg, logger)
	logger.Info("Cleaning up E2E cluster resources via Sweeper")
	err := sweeper.Run(ctx, cleanup.SweeperInput{ClusterName: e.TestResources.ClusterName})
	if err != nil {
		return fmt.Errorf("cleaning up e2e resources: %w", err)
	}

	logger.Info("Cleanup completed successfully")
	return nil
}

func (e *E2E) parseJSONReport(reportPath string) (E2EResult, error) {
	e2eResult := E2EResult{}
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		return e2eResult, fmt.Errorf("reading report: %w", err)
	}

	var reports []ginkgoTypes.Report
	err = json.Unmarshal(reportData, &reports)
	if err != nil {
		return e2eResult, fmt.Errorf("unmarshalling report: %w", err)
	}

	if len(reports) > 1 {
		return e2eResult, fmt.Errorf("multiple reports found, expected 1")
	}
	report := reports[0]
	e2eResult.TotalTests = report.PreRunStats.TotalSpecs
	e2eResult.TestRan = report.PreRunStats.SpecsThatWillRun

	if e.TestConfig.LogsBucket != "" {
		e2eResult.ArtifactsBucketPath = fmt.Sprintf("s3://%s/%s", e.TestConfig.LogsBucket, e.logsBucketPath())
	}

	// Process each failed test
	for _, spec := range report.SpecReports {
		if spec.State == ginkgoTypes.SpecStateSkipped || spec.State == ginkgoTypes.SpecStatePassed {
			continue
		}

		leafType := spec.LeafNodeType
		name := leafType.String()
		if leafType == ginkgoTypes.NodeTypeIt {
			name = strings.Join(spec.LeafNodeLabels, ", ")
		}

		// Get instance name from report entries
		var instanceName string
		for _, entry := range spec.ReportEntries {
			if entry.Name == constants.TestInstanceName {
				instanceName = entry.Value.String()
				break
			}
		}
		failedTest := FailedTest{
			InstanceName:   instanceName,
			Name:           name,
			State:          spec.State.String(),
			FailureMessage: specFailureMessage(spec),
		}

		if e.TestConfig.LogsBucket != "" && instanceName != "" {
			failedTest.ArtifactsBucketPath = fmt.Sprintf("s3://%s/%s/%s", e.TestConfig.LogsBucket, e.logsBucketPath(), instanceName)
		}

		e2eResult.FailedTests = append(e2eResult.FailedTests, failedTest)

		// Only process "It" test nodes for detailed logs
		if spec.LeafNodeType != ginkgoTypes.NodeTypeIt {
			continue
		}

		if failedTest.InstanceName == "" {
			err = errors.Join(err, fmt.Errorf("no instance name found for test"))
			continue
		}

		if saveErr := e.saveTestLogFile(spec, failedTest.InstanceName); saveErr != nil {
			err = errors.Join(err, fmt.Errorf("saving log file: %w", saveErr))
		}

	}
	if err != nil {
		e2eResult.RunFailureMessage = err.Error()
	}

	return e2eResult, nil
}

// saveTestLogFile creates a detailed log file for a test and uploads it to S3 if configured
// Returns the S3 path where the log was uploaded or an error
func (e *E2E) saveTestLogFile(spec ginkgoTypes.SpecReport, instanceName string) error {
	name := strings.Join(spec.LeafNodeLabels, ", ")

	logsDir := filepath.Join(e.TestConfig.ArtifactsFolder, instanceName)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("creating test logs directory: %w", err)
	}

	var logContent strings.Builder

	logContent.WriteString(fmt.Sprintf("Test: [%s]\n", name))
	logContent.WriteString(fmt.Sprintf("State: %s\n", spec.State.String()))
	logContent.WriteString(fmt.Sprintf("Duration: %.3f seconds\n", spec.RunTime.Seconds()))
	logContent.WriteString(fmt.Sprintf("Start Time: %s\n", spec.StartTime.Format(time.RFC3339)))
	logContent.WriteString(fmt.Sprintf("End Time: %s\n\n", spec.EndTime.Format(time.RFC3339)))

	if spec.CapturedStdOutErr != "" {
		logContent.WriteString("Captured StdOut/StdErr Output >>\n")
		logContent.WriteString(spec.CapturedStdOutErr)
		logContent.WriteString("\n\n")
	}

	logContent.WriteString(specFailureMessage(spec))

	logFilePath := filepath.Join(logsDir, testGinkgoOutputLog)
	if err := os.WriteFile(logFilePath, []byte(logContent.String()), 0o644); err != nil {
		return fmt.Errorf("writing test log file: %w", err)
	}
	// check if test created the serial-output.log file and copy it to the logs directory
	serialOutputLogFilePath := filepath.Join(e.TestConfig.ArtifactsFolder, fmt.Sprintf("%s-%s", instanceName, serialOutputLogFile))
	if _, err := os.Stat(serialOutputLogFilePath); err != nil {
		// sometimes the test will not produce the serial-output.log file
		// ok to ignore
		return nil
	}

	if err := os.Rename(serialOutputLogFilePath, filepath.Join(logsDir, serialOutputLogFile)); err != nil {
		return fmt.Errorf("moving serial output log file: %w", err)
	}

	return nil
}

func specFailureMessage(spec ginkgoTypes.SpecReport) string {
	if spec.Failure.Message == "" {
		return ""
	}
	sb := strings.Builder{}
	sb.WriteString("Failure Details >>\n")
	sb.WriteString(fmt.Sprintf("  [FAILED] %s\n", spec.LeafNodeText))
	sb.WriteString(fmt.Sprintf("  Expected %s\n", spec.Failure.Message))
	if spec.Failure.Location.FileName != "" {
		timestamp := spec.Failure.TimelineLocation.Time.Format(time.DateTime)
		sb.WriteString(fmt.Sprintf("  In [%s] at: %s:%d @ %s\n",
			spec.LeafNodeType.String(),
			spec.Failure.Location.FileName,
			spec.Failure.Location.LineNumber,
			timestamp))
	}
	sb.WriteString("\n")
	return sb.String()
}

func printE2EResultAsJSON(e2eResult E2EResult) error {
	jsonBytes, err := json.Marshal(e2eResult)
	if err != nil {
		return fmt.Errorf("marshalling e2e result: %w", err)
	}
	// not using logger to avoid the timestamp in the following output
	fmt.Printf("%s\n", string(jsonBytes))
	return nil
}

func (e *E2E) printE2EResultAsText(e2eResult E2EResult) error {
	// not using logger to avoid the timestamp in the following output
	fmt.Printf("%s\n", e2eResult.RunFailureMessage)
	fmt.Printf("%d/%d Tests ran\n\n", e2eResult.TestRan, e2eResult.TotalTests)
	if e2eResult.ArtifactsBucketPath != "" {
		fmt.Printf("Artifacts Bucket Path: %s\n", e.s3UriToConsoleUrl(e2eResult.ArtifactsBucketPath))
	}
	fmt.Printf("Ginkgo Test Log: %s\n", e2eResult.GinkgoLog)
	fmt.Printf("Setup Log: %s\n", e2eResult.SetupLog)
	fmt.Printf("Cleanup Log: %s\n\n", e2eResult.CleanupLog)

	if len(e2eResult.FailedTests) == 0 {
		fmt.Printf("All tests passed\n")
		return nil
	}
	fmt.Printf("Failed Phase: %s\n", e2eResult.LastPhase)

	fmt.Printf("Failed tests:\n")
	for _, failedTest := range e2eResult.FailedTests {
		fmt.Printf("\n\t[%s] - %s\n", failedTest.Name, failedTest.State)
		fmt.Printf("\tArtifacts Bucket Path: %s\n", e.s3UriToConsoleUrl(failedTest.ArtifactsBucketPath))
		fmt.Printf("\tGinkgo Test Log: %s\n", failedTest.GinkgoLog)
		fmt.Printf("\tSerial Log: %s\n", failedTest.SerialLog)
		if failedTest.CollectorLogsBundle != "" {
			fmt.Printf("\tLog Collector Bundle: %s\n", failedTest.CollectorLogsBundle)
		}
		if failedTest.FailureMessage != "" {
			fmt.Printf("\t%s\n", strings.ReplaceAll(failedTest.FailureMessage, "\n", "\n\t"))
		}
		fmt.Printf("\n")
	}
	return nil
}

func (e *E2E) uploadLogsToS3(ctx context.Context, s3Client *s3.Client, localFile, s3Key string) error {
	logContent, err := os.ReadFile(localFile)
	if err != nil {
		return fmt.Errorf("failed to read test log file: %w", err)
	}
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(e.TestConfig.LogsBucket),
		Key:    aws.String(s3Key),
		Body:   bytes.NewReader(logContent),
	})
	if err != nil {
		return fmt.Errorf("failed to upload test log to s3: %w", err)
	}
	return nil
}

func (e *E2E) logsBucketPath() string {
	return fmt.Sprintf("%s/%s", constants.TestS3LogsFolder, e.TestConfig.ClusterName)
}

func (e *E2E) updateE2EResultLogs(e2eResult E2EResult, s3Success bool) E2EResult {
	logPathPrefix := fmt.Sprintf("s3://%s/%s", e.TestConfig.LogsBucket, e.logsBucketPath())
	if !s3Success || e.TestConfig.LogsBucket == "" {
		logPathPrefix = e.TestConfig.ArtifactsFolder
	}
	e2eResult.GinkgoLog = fmt.Sprintf("%s/%s", logPathPrefix, testGinkgoOutputLog)
	e2eResult.SetupLog = fmt.Sprintf("%s/%s", logPathPrefix, testSetupLogFile)
	e2eResult.CleanupLog = fmt.Sprintf("%s/%s", logPathPrefix, testCleanupLogFile)
	for i, failedTest := range e2eResult.FailedTests {
		testLogPathPrefix := fmt.Sprintf("%s/%s", logPathPrefix, failedTest.InstanceName)
		failedTest.GinkgoLog = fmt.Sprintf("%s/%s", testLogPathPrefix, testGinkgoOutputLog)
		failedTest.SerialLog = fmt.Sprintf("%s/%s", testLogPathPrefix, serialOutputLogFile)
		failedTest.CollectorLogsBundle = fmt.Sprintf("%s/%s", testLogPathPrefix, constants.LogCollectorBundleFileName)
		e2eResult.FailedTests[i] = failedTest
	}
	return e2eResult
}

func (e *E2E) updateE2EResultPhase(phase string) {
	if e.TestsFailed {
		return
	}
	e.CurrentPhase = phase
}

func (e *E2E) s3UriToConsoleUrl(s3Uri string) string {
	if !strings.HasPrefix(s3Uri, "s3://") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(s3Uri, "s3://"), "/")
	if len(parts) < 2 {
		return ""
	}
	return fmt.Sprintf("https://%s.console.aws.amazon.com/s3/buckets/%s?prefix=%s/", e.TestConfig.ClusterRegion, parts[0], strings.Join(parts[1:], "/"))
}
