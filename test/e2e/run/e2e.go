package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const (
	e2eConfigFile                    = "e2e-param.yaml"
	e2eConfigFolder                  = "configs"
	e2eReportsFile                   = "junit-nodeadm.xml"
	e2eReportsFileJSON               = "junit-nodeadm.json"
	e2eReportsFolder                 = "reports"
	e2eTestResourcesFile             = "e2e-test-resources.yaml"
	phaseNameCleanupCluster          = "CleanupCluster"
	phaseNameExecuteTests            = "ExecuteTests"
	phaseNameParseReport             = "ParseReport"
	phaseNameSetupDirectories        = "SetupDirectories"
	phaseNameSetupTestInfrastructure = "SetupTestInfrastructure"
	phaseNameUploadArtifactsToS3     = "UploadArtifactsToS3"
	phaseNameWriteTestConfigs        = "WriteTestConfigs"
	reportingBuffer                  = time.Minute * 2
	testCleanupLogFile               = "cleanup-output.log"
	testGinkgoOutputLog              = "ginkgo-output.log"
	testSetupLogFile                 = "setup-output.log"
)

type Phase struct {
	Name           string `json:"name"`
	Error          error  `json:"-"`
	FailureMessage string `json:"failureMessage"`
	Status         string `json:"status"`
}

type E2EResult struct {
	ArtifactsBucketPath string       `json:"artifactsBucketPath"`
	CleanupLog          string       `json:"cleanupLog"`
	FailedTests         []FailedTest `json:"failedTests"`
	GinkgoLog           string       `json:"ginkgoLog"`
	// added as an indicator for log line to parse out to generate slack notification
	NodeadmE2eTestResultJSON bool    `json:"nodeadmE2eTestResultJSON"`
	Phases                   []Phase `json:"phases"`
	SetupLog                 string  `json:"setupLog"`
	TestFailed               int     `json:"testFailed"`
	TestRan                  int     `json:"testRan"`
	TotalTests               int     `json:"totalTests"`
}

type FailedTest struct {
	CollectorLogsBundle string `json:"collectorLogsBundle"`
	FailureMessage      string `json:"failureMessage"`
	InstanceName        string `json:"instanceName"`
	GinkgoLog           string `json:"ginkgoLog"`
	Name                string `json:"name"`
	NodeadmVersion      string `json:"nodeadmVersion"`
	SerialLog           string `json:"serialLog"`
	State               string `json:"state"`
}

type E2EPaths struct {
	CleanupLog      string
	Configs         string
	Ginkgo          string
	GinkgoOutputLog string
	Reports         string
	ReportsFileJSON string
	SetupLog        string
	// prefix (path) for logs/artifacts on S3, instance name will be appended to this path
	// ex: logs/<cluster-name>
	LogsBucketPath string
	// either nodeadm.test or ./test/e2e/suite/nodeadm
	TestsBinaryOrSource string
	// path to test config file e2e.TestConfig
	TestConfigFile string
	// path to test resources file cluster.TestResources
	TestResourcesFile string
}

type E2E struct {
	AwsCfg          aws.Config
	Logger          logr.Logger
	NoColor         bool
	Paths           E2EPaths
	TestConfig      e2e.TestConfig
	TestLabelFilter string
	TestProcs       int
	Timeout         time.Duration
	TestResources   cluster.TestResources
	SkipCleanup     bool
	SkippedTests    string
}

func (e *E2E) Run(ctx context.Context) (E2EResult, error) {
	deadline := time.Now().Add(e.Timeout)
	ctx, cancelFunc := context.WithDeadline(ctx, deadline)
	defer cancelFunc()

	e.initPaths()

	phases, err := e.preTestSetup()
	if err != nil {
		return E2EResult{Phases: phases}, err
	}

	// save 5 minutes for cleanup and 2 minutes for reporting/s3 uploading
	runTestsDeadline := deadline.Add(-reportingBuffer)
	if !e.SkipCleanup {
		runTestsDeadline = runTestsDeadline.Add(-cleanupBuffer)
	}
	runTestsCtx, runTestsCancelFunc := context.WithDeadline(ctx, runTestsDeadline)
	defer runTestsCancelFunc()
	runTestsPhases := e.runTests(runTestsCtx)
	phases = append(phases, runTestsPhases...)

	// save 2 minutes for reporting/s3 uploading
	cleanupDeadline := deadline.Add(-reportingBuffer)
	cleanupCtx, cleanupCancelFunc := context.WithDeadline(ctx, cleanupDeadline)
	defer cleanupCancelFunc()
	cleanupPhases := e.runCleanup(cleanupCtx)
	phases = append(phases, cleanupPhases...)

	e2eResult, parsePhases := e.parseReport(ctx)
	phases = append(phases, parsePhases...)

	uploadPhases := e.uploadArtifactsToS3(ctx)
	phases = append(phases, uploadPhases...)

	e2eResult.Phases = phases
	var allErrors error
	for _, phase := range phases {
		if phase.Error != nil {
			allErrors = errors.Join(allErrors, phase.Error)
		}
	}
	return e2eResult, allErrors
}

func (e *E2E) initPaths() {
	e.Paths.Configs = filepath.Join(e.TestConfig.ArtifactsFolder, e2eConfigFolder)
	e.Paths.TestConfigFile = filepath.Join(e.Paths.Configs, e2eConfigFile)
	e.Paths.LogsBucketPath = filepath.Join(constants.TestS3LogsFolder, e.TestConfig.ClusterName)
	e.Paths.Reports = filepath.Join(e.TestConfig.ArtifactsFolder, e2eReportsFolder)
	e.Paths.ReportsFileJSON = filepath.Join(e.Paths.Reports, e2eReportsFileJSON)
	e.Paths.TestResourcesFile = filepath.Join(e.Paths.Configs, e2eTestResourcesFile)
	e.Paths.GinkgoOutputLog = filepath.Join(e.TestConfig.ArtifactsFolder, testGinkgoOutputLog)
	e.Paths.CleanupLog = filepath.Join(e.TestConfig.ArtifactsFolder, testCleanupLogFile)
	e.Paths.SetupLog = filepath.Join(e.TestConfig.ArtifactsFolder, testSetupLogFile)
}

func phaseCompleted(phases []Phase, name, message string, err error) []Phase {
	phase := Phase{Name: name, Status: "success"}
	if err != nil {
		phase.Error = fmt.Errorf("%s: %w", message, err)
		phase.FailureMessage = err.Error()
		phase.Status = "failure"
	}
	return append(phases, phase)
}

// preTestSetup sets up the directories and writes the test configs
// it returns the failed phase and an error if one occurred
func (e *E2E) preTestSetup() ([]Phase, error) {
	err := e.setupDirectories()
	phases := phaseCompleted([]Phase{}, phaseNameSetupDirectories, "setting up directories", err)
	if err != nil {
		return phases, err
	}

	err = e.writeTestConfigs()
	phases = phaseCompleted(phases, phaseNameWriteTestConfigs, "creating test config", err)
	if err != nil {
		return phases, err
	}

	return phases, nil
}

func (e *E2E) runTests(ctx context.Context) []Phase {
	runner := E2ERunner{
		AwsCfg:          e.AwsCfg,
		Logger:          e.Logger,
		NoColor:         e.NoColor,
		Paths:           e.Paths,
		TestLabelFilter: e.TestLabelFilter,
		TestProcs:       e.TestProcs,
		TestResources:   e.TestResources,
		SkippedTests:    e.SkippedTests,
	}
	return runner.Run(ctx)
}

func (e *E2E) runCleanup(ctx context.Context) []Phase {
	if e.SkipCleanup {
		e.Logger.Info("Skipping cluster and infrastructure cleanup via stack deletion")
		return nil
	}

	// We use a custom AppId so the requests show that they were
	// made by this cleanup in the user-agent
	aws := e.AwsCfg.Copy()
	aws.AppID = "nodeadm-e2e-test-run-cleanup"
	cleaner := E2ECleanup{
		AwsCfg:        aws,
		Logger:        newFileLogger(e.Paths.CleanupLog, e.NoColor),
		TestResources: e.TestResources,
	}
	return cleaner.Run(ctx)
}

// parseReport parses the report and returns the E2EResult
// on error an empty E2EResult is returned
func (e *E2E) parseReport(ctx context.Context) (E2EResult, []Phase) {
	report := E2EReport{
		ArtifactsFolder: e.TestConfig.ArtifactsFolder,
	}

	e2eResult, err := report.Parse(ctx, e.Paths.ReportsFileJSON)
	phases := phaseCompleted([]Phase{}, phaseNameParseReport, "parsing report", err)
	if err != nil {
		e.Logger.Error(err, "Failed to parse report")
	}
	return e2eResult, phases
}

func (e *E2E) uploadArtifactsToS3(ctx context.Context) []Phase {
	artifacts := E2EArtifacts{
		ArtifactsFolder: e.TestConfig.ArtifactsFolder,
		AwsCfg:          e.AwsCfg,
		Logger:          e.Logger,
		LogsBucket:      e.TestConfig.LogsBucket,
		LogsBucketPath:  e.Paths.LogsBucketPath,
	}
	err := artifacts.Upload(ctx)
	phases := phaseCompleted([]Phase{}, phaseNameUploadArtifactsToS3, "uploading artifacts to s3", err)
	if err != nil {
		e.Logger.Error(err, "Failed to upload artifacts to s3")
	}
	return phases
}

func (e *E2E) PrintResults(ctx context.Context, e2eResult E2EResult) error {
	// not using logger when outputting json/text results to avoid the timestamp in the following output
	output := E2EOutput{
		ArtifactsBucketPath: e2eResult.ArtifactsBucketPath,
		ClusterRegion:       e.TestConfig.ClusterRegion,
	}
	var jsonErr error
	if err := output.PrintJSON(e2eResult); err != nil {
		jsonErr = fmt.Errorf("printing e2e result as json: %w", err)
	}
	fmt.Printf("\n")
	output.PrintText(e2eResult)
	return jsonErr
}

func (e *E2E) setupDirectories() error {
	if err := os.MkdirAll(e.Paths.Configs, 0o755); err != nil {
		return fmt.Errorf("creating test config directory: %w", err)
	}

	if err := os.MkdirAll(e.Paths.Reports, 0o755); err != nil {
		return fmt.Errorf("creating test reports directory: %w", err)
	}

	return nil
}

func (e *E2E) writeTestConfigs() error {
	paramsBytes, err := yaml.Marshal(e.TestConfig)
	if err != nil {
		return fmt.Errorf("marshaling params: %w", err)
	}

	if err := os.WriteFile(e.Paths.TestConfigFile, paramsBytes, 0o644); err != nil {
		return fmt.Errorf("writing params: %w", err)
	}

	testResourcesBytes, err := yaml.Marshal(e.TestResources)
	if err != nil {
		return fmt.Errorf("marshaling test resources: %w", err)
	}

	// not needed for the test run, but useful for debugging
	if err := os.WriteFile(e.Paths.TestResourcesFile, testResourcesBytes, 0o644); err != nil {
		return fmt.Errorf("writing test resources: %w", err)
	}
	return nil
}

func newFileLogger(fileName string, noColor bool) logr.Logger {
	return e2e.NewLogger(e2e.LoggerConfig{NoColor: noColor}, e2e.WithOutputFile(fileName))
}
