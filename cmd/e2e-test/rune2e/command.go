package rune2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/go-logr/logr"
	"github.com/integrii/flaggy"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
	"github.com/aws/eks-hybrid/test/e2e/run"
)

const (
	defaultNodeadmAMDURL     = "https://hybrid-assets.eks.amazonaws.com/releases/latest/bin/linux/amd64/nodeadm"
	defaultNodeadmARMURL     = "https://hybrid-assets.eks.amazonaws.com/releases/latest/bin/linux/arm64/nodeadm"
	defaultClusterNamePrefix = "nodeadm-e2e-tests"
	defaultRegion            = "us-west-2"
	defaultK8sVersion        = "1.31"
	defaultCNI               = "cilium"
	defaultTimeout           = time.Minute * 60
	defaultTestProcs         = 1
	defaultTestsBinaryOrPath = "./test/e2e/suite/nodeadm"
)

type command struct {
	artifactsDir      string
	clusterName       string
	cni               string
	ginkgoBinaryPath  string
	k8sVersion        string
	logsBucket        string
	noColor           bool
	nodeadmAMDURL     string
	nodeadmARMURL     string
	region            string
	setupConfigFile   string
	skipCleanup       bool
	skippedTests      string
	subCmd            *flaggy.Subcommand
	testConfigFile    string
	testLabelFilter   string
	testProcs         int
	testsBinaryOrPath string
	timeout           time.Duration
}

func NewCommand() *command {
	cmd := &command{
		clusterName:       fmt.Sprintf("%s-%s", defaultClusterNamePrefix, strings.ReplaceAll(defaultK8sVersion, ".", "-")),
		cni:               defaultCNI,
		k8sVersion:        defaultK8sVersion,
		nodeadmAMDURL:     defaultNodeadmAMDURL,
		nodeadmARMURL:     defaultNodeadmARMURL,
		skipCleanup:       false,
		subCmd:            flaggy.NewSubcommand("run-e2e"),
		testProcs:         defaultTestProcs,
		timeout:           defaultTimeout,
		testsBinaryOrPath: defaultTestsBinaryOrPath,
	}
	cmd.subCmd.Description = "Run E2E tests"
	cmd.subCmd.String(&cmd.clusterName, "n", "name", "Cluster name (optional)")
	cmd.subCmd.String(&cmd.region, "r", "region", "AWS region (optional)")
	cmd.subCmd.String(&cmd.k8sVersion, "k", "kubernetes-version", "Kubernetes version (optional)")
	cmd.subCmd.String(&cmd.cni, "c", "cni", "CNI plugin (optional)")
	cmd.subCmd.String(&cmd.nodeadmAMDURL, "", "nodeadm-amd-url", "NodeADM AMD URL (optional)")
	cmd.subCmd.String(&cmd.nodeadmARMURL, "", "nodeadm-arm-url", "NodeADM ARM URL (optional)")
	cmd.subCmd.String(&cmd.logsBucket, "b", "logs-bucket", "S3 bucket for logs (optional)")
	cmd.subCmd.String(&cmd.artifactsDir, "a", "artifacts-dir", "Directory for artifacts (optional, defaults to a new temp directory)")
	cmd.subCmd.String(&cmd.skippedTests, "s", "skipped-tests", "ginkgo regex to skip tests (optional)")
	cmd.subCmd.Duration(&cmd.timeout, "", "timeout", "Timeout for the test (optional)")
	cmd.subCmd.String(&cmd.testLabelFilter, "f", "test-filter", "Filter for the test (optional)")
	cmd.subCmd.Bool(&cmd.skipCleanup, "", "skip-cleanup", "Skip cleanup (optional)")
	cmd.subCmd.String(&cmd.testsBinaryOrPath, "", "tests-binary", "Path to the tests binary (optional)")
	cmd.subCmd.Bool(&cmd.noColor, "", "no-color", "Disable color output (optional)")
	cmd.subCmd.Int(&cmd.testProcs, "p", "procs", "Number of processes to run (optional)")
	cmd.subCmd.String(&cmd.setupConfigFile, "", "setup-config", "Path to a YAML file containing cluster.TestResources configuration (optional)")
	cmd.subCmd.String(&cmd.testConfigFile, "", "test-config", "Path to a YAML file containing suite.TestConfig configuration (optional)")
	cmd.subCmd.String(&cmd.ginkgoBinaryPath, "", "ginkgo-binary", "Path to the ginkgo binary (defaults to the ginkgo binary in the same folder as e2e-test or in the PATH)")
	return cmd
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.subCmd
}

func (c *command) Commands() []cli.Command {
	return []cli.Command{c}
}

func (c *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	logger := e2e.NewLogger(e2e.LoggerConfig{NoColor: c.noColor})

	awsCfg, err := e2e.NewAWSConfig(ctx,
		config.WithRegion(c.region),
		// We use a custom AppId so the requests show that they were
		// made by this command in the user-agent
		config.WithAppID("nodeadm-e2e-test-run-cmd"),
		config.WithRetryer(func() aws.Retryer {
			return retry.AddWithMaxBackoffDelay(
				retry.AddWithMaxAttempts(
					retry.NewStandard(),
					10, // Max 10 attempts
				),
				10*time.Second, // Max backoff delay
			)
		}),
	)
	if err != nil {
		return fmt.Errorf("reading AWS configuration: %w", err)
	}
	if c.region == "" {
		c.region = awsCfg.Region
	}

	testResources, err := c.loadSetupConfig(logger)
	if err != nil {
		return fmt.Errorf("loading test resources configuration: %w", err)
	}

	testConfig, err := c.loadTestConfig(testResources, logger)
	if err != nil {
		return fmt.Errorf("loading test configuration: %w", err)
	}

	artifactsDir, err := c.getArtifactsDir(c.artifactsDir, logger)
	if err != nil {
		return fmt.Errorf("getting artifacts directory: %w", err)
	}

	testConfig.ArtifactsFolder = artifactsDir

	ginkgoBinaryPath, err := c.getGinkgoBinaryPath()
	if err != nil {
		return fmt.Errorf("getting ginkgo binary path: %w", err)
	}

	// Run E2E tests
	e2e := run.E2E{
		AwsCfg:  awsCfg,
		Logger:  logger,
		NoColor: c.noColor,
		Paths: run.E2EPaths{
			Ginkgo:              ginkgoBinaryPath,
			TestsBinaryOrSource: c.testsBinaryOrPath,
		},
		TestConfig:      testConfig,
		TestLabelFilter: c.testLabelFilter,
		TestProcs:       c.testProcs,
		Timeout:         c.timeout,
		TestResources:   testResources,
		SkipCleanup:     c.skipCleanup,
		SkippedTests:    c.skippedTests,
	}

	e2eResult, testErr := e2e.Run(ctx)

	// Always try to output the results
	outputErr := e2e.PrintResults(ctx, e2eResult)
	if outputErr != nil {
		logger.Error(outputErr, "outputting E2E results")
	}
	if testErr != nil {
		return fmt.Errorf("running E2E tests: %w", testErr)
	}

	return nil
}

func (c *command) getArtifactsDir(artifactsDir string, logger logr.Logger) (string, error) {
	var err error
	if artifactsDir == "" {
		artifactsDir, err = os.MkdirTemp("", "eks-hybrid-e2e-*")
		if err != nil {
			return "", fmt.Errorf("creating temp directory: %w", err)
		}
		logger.Info("Created temporary test directory", "path", artifactsDir)
	}
	artifactsDir, err = filepath.Abs(artifactsDir)
	if err != nil {
		return "", fmt.Errorf("getting absolute path for artifacts: %w", err)
	}
	return artifactsDir, nil
}

// loadSetupConfig loads the TestResources configuration from a file.
// It validates that no individual resource flags are set when using a config file.
func (c *command) loadSetupConfig(logger logr.Logger) (cluster.TestResources, error) {
	// Initialize default test resources
	testResources := cluster.TestResources{
		ClusterName:       c.clusterName,
		ClusterRegion:     c.region,
		KubernetesVersion: c.k8sVersion,
		Cni:               c.cni,
	}

	testResources = cluster.SetTestResourcesDefaults(testResources)

	if c.setupConfigFile != "" {
		// Validate that individual resource flags are not also set
		defaultClusterName := fmt.Sprintf("%s-%s", defaultClusterNamePrefix, strings.ReplaceAll(defaultK8sVersion, ".", "-"))
		if c.clusterName != defaultClusterName ||
			c.region != defaultRegion ||
			c.k8sVersion != defaultK8sVersion ||
			c.cni != defaultCNI {
			return testResources, fmt.Errorf("cannot specify both setup-config file and individual cluster resource flags (name, region, kubernetes-version, cni, eks-endpoint)")
		}

		// Load test resources from file
		var err error
		testResources, err = cluster.LoadTestResources(c.setupConfigFile)
		if err != nil {
			return testResources, fmt.Errorf("reading setup config file: %w", err)
		}

		logger.Info("Loaded test resources configuration from file", "path", c.setupConfigFile)
	}

	return testResources, nil
}

// loadTestConfig loads the TestConfig configuration from a file.
// It validates that no individual test config flags are set when using a config file.
func (c *command) loadTestConfig(testResources cluster.TestResources, logger logr.Logger) (e2e.TestConfig, error) {
	testConfig := e2e.TestConfig{
		ClusterName:   testResources.ClusterName,
		ClusterRegion: testResources.ClusterRegion,
		Endpoint:      testResources.EKS.Endpoint,
		NodeadmUrlAMD: c.nodeadmAMDURL,
		NodeadmUrlARM: c.nodeadmARMURL,
		LogsBucket:    c.logsBucket,
	}

	if c.testConfigFile != "" {
		// Validate that individual test config flags are not also set
		if c.nodeadmAMDURL != defaultNodeadmAMDURL ||
			c.nodeadmARMURL != defaultNodeadmARMURL ||
			c.logsBucket != "" ||
			c.artifactsDir != "" {
			return testConfig, fmt.Errorf("cannot specify both test-config file and individual test config flags (nodeadm-amd-url, nodeadm-arm-url, logs-bucket, artifacts-dir)")
		}

		// Load test config from file
		testConfigData, err := os.ReadFile(c.testConfigFile)
		if err != nil {
			return testConfig, fmt.Errorf("reading test config file: %w", err)
		}

		if err := yaml.Unmarshal(testConfigData, &testConfig); err != nil {
			return testConfig, fmt.Errorf("unmarshaling test config: %w", err)
		}

		logger.Info("Loaded test configuration from file", "path", c.testConfigFile)
	}

	return testConfig, nil
}

func (c *command) getGinkgoBinaryPath() (string, error) {
	if c.ginkgoBinaryPath != "" {
		return c.ginkgoBinaryPath, nil
	}

	ex, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("getting executable path: %w", err)
	}

	binPath := filepath.Dir(ex)
	ginkgoBinaryPath := filepath.Join(binPath, "ginkgo")

	_, err = os.Stat(ginkgoBinaryPath)
	if err == nil {
		return ginkgoBinaryPath, nil
	}

	// fallback to ginkgo in PATH
	return "ginkgo", nil
}
