//go:build e2e
// +build e2e

package mixed_mode

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"math/rand"
	"strings"
	"testing"
	"time"

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2v2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	smithyTime "github.com/aws/smithy-go/time"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/suite"
)

// SharedTestData holds all data that needs to be shared between Ginkgo processes
type SharedTestData struct {
	SuiteConfig          suite.SuiteConfiguration `yaml:"suiteConfig"`
	MixedModeLabels      map[string]string        `yaml:"mixedModeLabels"`
	HybridNodeName       string                   `yaml:"hybridNodeName"`
	ManagedNodeGroupName string                   `yaml:"managedNodeGroupName"`
	TestRunId            string                   `yaml:"testRunId"`
}

var (
	filePath       string
	sharedTestData SharedTestData

	// Namespace constants
	testNamespace = "default"

	// Network CIDR constants (match defaults from create.go)
	cloudVPCCIDR  = "10.20.0.0/16"
	hybridVPCCIDR = "10.80.0.0/16"
	podCIDR       = "10.87.0.0/16"

	// Port constants
	httpPort    int32 = 80
	dnsPort     int32 = 53
	webhookPort int32 = 443

	// Timing constants
	crossVPCPropagationWait = 180 * time.Second

	// Node selector constants
	cloudNodeSelector  = map[string]string{"node.kubernetes.io/instance-type": "m5.large"}
	hybridNodeSelector = map[string]string{"eks.amazonaws.com/compute-type": "hybrid"}

	// Node label constants (derived from selectors)
	cloudNodeLabelKey, cloudNodeLabelValue   = getFirstKeyValue(cloudNodeSelector)
	hybridNodeLabelKey, hybridNodeLabelValue = getFirstKeyValue(hybridNodeSelector)
)

// getFirstKeyValue extracts the first key-value pair from a map
func getFirstKeyValue(selector map[string]string) (string, string) {
	for k, v := range selector {
		return k, v
	}
	return "", ""
}

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestMixedModeE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mixed Mode E2E Suite")
}

var _ = SynchronizedBeforeSuite(
	// This function only runs once, on the first process
	func(ctx context.Context) []byte {
		suiteConfig := suite.BeforeSuiteCredentialSetup(ctx, filePath)

		test := suite.BeforeVPCTest(ctx, &suiteConfig)
		credentialProviders := suite.AddClientsToCredentialProviders(suite.CredentialProviders(), test)
		osProviderList := suite.OSProviderList(credentialProviders)
		randomOSProvider := osProviderList[rand.Intn(len(osProviderList))]

		// Generate unique identifiers for this test run
		timestamp := time.Now().Format("20060102-150405")
		version := strings.ReplaceAll(test.Cluster.KubernetesVersion, ".", "")
		testRunId := fmt.Sprintf("k8s%s-%s", version, timestamp)
		hybridNodeName := fmt.Sprintf("mixed-mode-hybrid-%s", testRunId)
		managedNodeGroupName := fmt.Sprintf("mixed-mode-cloud-%s", testRunId)

		test.Logger.Info("Generated unique identifiers", "testRunId", testRunId, "hybridNodeName", hybridNodeName, "managedNodeGroupName", managedNodeGroupName)

		// Create labels with kubernetes version
		mixedModeLabels := map[string]string{
			"test-suite":         "mixed-mode",
			"kubernetes-version": test.Cluster.KubernetesVersion,
		}

		// Global cleanup of k8s resources in case we are reusing the cluster from a previous run
		test.Logger.Info("Running global cleanup before test suite starts")
		cleanupTestResources(ctx, test, mixedModeLabels)

		hybridNode := suite.NodeCreate{
			InstanceName: hybridNodeName,
			InstanceSize: e2e.Large,
			NodeName:     hybridNodeName,
			OS:           randomOSProvider.OS,
			Provider:     randomOSProvider.Provider,
			ComputeType:  e2e.CPUInstance,
		}
		suite.CreateNodes(ctx, test, []suite.NodeCreate{hybridNode})

		Expect(test.CreateManagedNodeGroups(ctx)).To(Succeed(), "managed node group should be created successfully")

		// Ensure mixed mode connectivity by adding required security group rules
		err := ensureMixedModeConnectivity(ctx, test, hybridNodeName)
		Expect(err).NotTo(HaveOccurred(), "Mixed mode connectivity rules should be added successfully")

		// Package all shared data for distribution to test processes
		sharedData := SharedTestData{
			SuiteConfig:          suiteConfig,
			MixedModeLabels:      mixedModeLabels,
			HybridNodeName:       hybridNodeName,
			ManagedNodeGroupName: managedNodeGroupName,
			TestRunId:            testRunId,
		}

		test.Logger.Info("Sharing test data with all processes", "labels", mixedModeLabels, "hybridNodeName", hybridNodeName)

		sharedJson, err := yaml.Marshal(sharedData)
		Expect(err).NotTo(HaveOccurred(), "shared data should be marshalled successfully")
		return sharedJson
	},
	// This function runs on all processes
	func(ctx context.Context, data []byte) {
		// add a small sleep to add jitter to the start of each test
		randomSleep := rand.Intn(10)
		err := smithyTime.SleepWithContext(ctx, time.Duration(randomSleep)*time.Second)
		Expect(err).NotTo(HaveOccurred(), "failed to sleep")

		// Unmarshal the shared data
		err = yaml.Unmarshal(data, &sharedTestData)
		Expect(err).NotTo(HaveOccurred(), "shared data should be unmarshalled successfully")
	},
)

var _ = Describe("Mixed Mode Testing", func() {
	When("hybrid and cloud-managed nodes coexist", func() {
		var test *suite.PeeredVPCTest
		var testCaseLabels map[string]string

		BeforeEach(func(ctx context.Context) {
			test = suite.BeforeVPCTest(ctx, &sharedTestData.SuiteConfig)

			// Create unique labels for this specific test case
			testCaseLabels = maps.Clone(sharedTestData.MixedModeLabels)
		})

		AfterEach(func(ctx context.Context) {
			// Clean up using the test-case-specific labels if available
			if testCaseLabels != nil {
				test.Logger.Info("Running comprehensive cleanup after test")
				cleanupTestResources(ctx, test, testCaseLabels)
			}
		})

		Context("Pod-to-Pod Communication", func() {
			It("should enable cross-VPC pod-to-pod communication from hybrid to cloud nodes", func(ctx context.Context) {
				testCaseLabels["test-case"] = "pod-hybrid-to-cloud"

				// Find cloud node and create nginx pod
				cloudNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, cloudNodeLabelKey, cloudNodeLabelValue, test.Logger)

				err := kubernetes.CreateNginxPodInNode(ctx, test.K8sClient.Interface, cloudNodeName, testNamespace, test.Cluster.Region, test.Logger, "nginx-cloud", testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "should create nginx pod 'nginx-cloud' on cloud node %s", cloudNodeName)
				test.Logger.Info("Cloud pod created and ready", "name", "nginx-cloud")

				// Find hybrid node and create client nginx pod
				hybridNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, hybridNodeLabelKey, hybridNodeLabelValue, test.Logger)

				err = kubernetes.CreateNginxPodInNode(ctx, test.K8sClient.Interface, hybridNodeName, testNamespace, test.Cluster.Region, test.Logger, "test-client-hybrid", testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "should create client pod 'test-client-hybrid' on hybrid node %s", hybridNodeName)
				test.Logger.Info("Client pod created and ready", "name", "test-client-hybrid")

				test.Logger.Info("Testing cross-VPC pod-to-pod connectivity (hybrid → cloud)")
				err = kubernetes.TestPodToPodConnectivity(ctx, test.K8sClientConfig, test.K8sClient.Interface, "test-client-hybrid", "nginx-cloud", testNamespace, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing pod-to-pod connectivity")

				test.Logger.Info("Cross-VPC pod-to-pod communication test completed successfully")
			})

			It("should enable cross-VPC pod-to-pod communication from cloud to hybrid nodes", func(ctx context.Context) {
				testCaseLabels["test-case"] = "pod-cloud-to-hybrid"

				// Find hybrid node and create nginx pod
				hybridNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, hybridNodeLabelKey, hybridNodeLabelValue, test.Logger)

				err := kubernetes.CreateNginxPodInNode(ctx, test.K8sClient.Interface, hybridNodeName, testNamespace, test.Cluster.Region, test.Logger, "nginx-hybrid-reverse", testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "should create nginx pod 'nginx-hybrid-reverse' on hybrid node %s", hybridNodeName)
				test.Logger.Info("Hybrid pod created and ready", "name", "nginx-hybrid-reverse")

				// Find cloud node and create client nginx pod
				cloudNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, cloudNodeLabelKey, cloudNodeLabelValue, test.Logger)

				err = kubernetes.CreateNginxPodInNode(ctx, test.K8sClient.Interface, cloudNodeName, testNamespace, test.Cluster.Region, test.Logger, "test-client-cloud", testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "should create client pod 'test-client-cloud' on cloud node %s", cloudNodeName)
				test.Logger.Info("Cloud client pod created and ready", "name", "test-client-cloud")

				err = kubernetes.TestPodToPodConnectivity(ctx, test.K8sClientConfig, test.K8sClient.Interface, "test-client-cloud", "nginx-hybrid-reverse", testNamespace, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing pod-to-pod connectivity")

				test.Logger.Info("Cross-VPC pod-to-pod communication test completed successfully")
			})
		})

		Context("Cross-VPC Service Discovery", func() {
			It("should enable cross-VPC service discovery from hybrid to cloud services", func(ctx context.Context) {
				testCaseLabels["test-case"] = "service-hybrid-to-cloud"

				// Create deployment
				_, err := kubernetes.CreateDeployment(ctx, test.K8sClient.Interface, "nginx-service-cloud", testNamespace, test.Cluster.Region, cloudNodeSelector, httpPort, 1, test.Logger, testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "creating deployment")

				// Create service (port 80 to target port 80)
				service, err := kubernetes.CreateService(ctx, test.K8sClient.Interface, "nginx-service-cloud", testNamespace, map[string]string{"app": "nginx-service-cloud"}, httpPort, httpPort, test.Logger, testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "creating service")

				// Find hybrid node and create client nginx pod
				hybridNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, hybridNodeLabelKey, hybridNodeLabelValue, test.Logger)

				err = kubernetes.CreateNginxPodInNode(ctx, test.K8sClient.Interface, hybridNodeName, testNamespace, test.Cluster.Region, test.Logger, "test-client-hybrid-service", testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "creating client pod")
				test.Logger.Info("Client pod created and ready", "name", "test-client-hybrid-service")

				err = kubernetes.WaitForServiceReady(ctx, test.K8sClient.Interface, service.Name, testNamespace, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "waiting for service to be ready")

				// Allow time for network rules and DNS to fully propagate across VPCs
				test.Logger.Info("Waiting for network rules and DNS to propagate across VPCs")
				time.Sleep(crossVPCPropagationWait)

				// Test service connectivity with integrated DNS resolution testing (port 80)
				err = kubernetes.TestServiceConnectivityWithRetries(ctx, test.K8sClientConfig, test.K8sClient.Interface, "test-client-hybrid-service", service.Name, testNamespace, httpPort, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing service connectivity")

				test.Logger.Info("Cross-VPC service discovery test (hybrid → cloud) completed successfully")
			})

			It("should enable cross-VPC service discovery from cloud to hybrid services", func(ctx context.Context) {
				testCaseLabels["test-case"] = "service-cloud-to-hybrid"

				test.Logger.Info("Testing bidirectional service discovery (cloud → hybrid service)")

				_, err := kubernetes.CreateDeployment(ctx, test.K8sClient.Interface, "nginx-service-hybrid", testNamespace, test.Cluster.Region, hybridNodeSelector, httpPort, 1, test.Logger, testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "creating deployment")

				service, err := kubernetes.CreateService(ctx, test.K8sClient.Interface, "nginx-service-hybrid", testNamespace, map[string]string{"app": "nginx-service-hybrid"}, httpPort, httpPort, test.Logger, testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "creating service")

				// Find cloud node and create client nginx pod
				cloudNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, cloudNodeLabelKey, cloudNodeLabelValue, test.Logger)

				err = kubernetes.CreateNginxPodInNode(ctx, test.K8sClient.Interface, cloudNodeName, testNamespace, test.Cluster.Region, test.Logger, "test-client-cloud-bidirectional", testCaseLabels)
				Expect(err).NotTo(HaveOccurred(), "creating client pod")
				test.Logger.Info("Client pod created and ready", "name", "test-client-cloud-bidirectional")

				err = kubernetes.WaitForServiceReady(ctx, test.K8sClient.Interface, service.Name, testNamespace, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "waiting for service to be ready")
				// Allow time for network rules and DNS to fully propagate across VPCs
				test.Logger.Info("Waiting for network rules and DNS to propagate across VPCs")
				time.Sleep(crossVPCPropagationWait)
				// Test service connectivity with integrated DNS resolution testing (port 80)
				err = kubernetes.TestServiceConnectivityWithRetries(ctx, test.K8sClientConfig, test.K8sClient.Interface, "test-client-cloud-bidirectional", service.Name, testNamespace, httpPort, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing service connectivity")

				test.Logger.Info("Bidirectional service discovery test (cloud → hybrid) completed successfully")
			})
		})

		Context("CloudWatch Observability Mixed Mode", func() {
			It("should support CloudWatch Observability in mixed mode", func(ctx context.Context) {
				testCaseLabels["test-case"] = "cloudwatch-mixed-mode"

				test.Logger.Info("Starting mixed mode test with CloudWatch Observability addon")

				podIdentityRoleArn, err := addon.PodIdentityRole(ctx, test.IAMClient, test.Cluster.Name)
				Expect(err).NotTo(HaveOccurred(), "should get pod identity role ARN")

				test.Logger.Info("Retrieved pod identity role with CloudWatch Logs permissions", "roleArn", podIdentityRoleArn)

				cloudwatchAddon := addon.NewCloudWatchAddon(test.Cluster.Name, podIdentityRoleArn)

				Expect(cloudwatchAddon).NotTo(BeNil(), "CloudWatch addon should be created")
				Expect(cloudwatchAddon.PodIdentityRoleArn).To(Equal(podIdentityRoleArn), "IRSA role should be configured")

				DeferCleanup(func(ctx context.Context) {
					Expect(cloudwatchAddon.Delete(ctx, test.EKSClient, test.Logger)).To(Succeed(), "should cleanup CloudWatch addon successfully")
				})

				err = cloudwatchAddon.SetupCwAddon(
					ctx,
					test.EKSClient,
					test.K8sClient.Interface,
					test.CloudWatchLogsClient,
					test.Logger,
				)
				Expect(err).NotTo(HaveOccurred(), "should setup CloudWatch addon successfully")

				test.Logger.Info("CloudWatch addon installed successfully")

				err = cloudwatchAddon.VerifyCwAddon(
					ctx,
					test.K8sClient.Dynamic,
					test.CloudWatchLogsClient,
					test.Logger,
				)
				Expect(err).NotTo(HaveOccurred(), "should verify CloudWatch addon successfully")

				test.Logger.Info("CloudWatch Observability mixed mode test successful")
			})
		})

		Context("Pod Identity Agent Mixed Mode", func() {
			It("should support Pod Identity Agent functionality in mixed mode", func(ctx context.Context) {
				testCaseLabels["test-case"] = "podidentity-mixed-mode"

				test.Logger.Info("Starting mixed mode test with Pod Identity addon")

				// Step 2: Test Pod Identity on hybrid node
				hybridNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, hybridNodeLabelKey, hybridNodeLabelValue, test.Logger)
				hybridVerifier := test.NewVerifyPodIdentityAddon(hybridNodeName)
				err := hybridVerifier.Run(ctx)
				Expect(err).NotTo(HaveOccurred(), "Pod Identity should work on hybrid node")

				test.Logger.Info("Pod Identity test on hybrid node completed successfully")

				// Step 3: Test Pod Identity on cloud node
				cloudNodeName, _ := kubernetes.FindNodeWithLabel(ctx, test.K8sClient.Interface, cloudNodeLabelKey, cloudNodeLabelValue, test.Logger)
				cloudVerifier := test.NewVerifyPodIdentityAddon(cloudNodeName)
				err = cloudVerifier.Run(ctx)
				Expect(err).NotTo(HaveOccurred(), "Pod Identity should work on cloud node")

				test.Logger.Info("Pod Identity mixed mode test successful - verified on both hybrid and cloud nodes")
			})
		})

		Context("cert-manager Mixed Mode", func() {
			It("should support cert-manager webhook functionality in mixed mode", func(ctx context.Context) {
				testCaseLabels["test-case"] = "certmanager-mixed-mode"

				test.Logger.Info("Starting mixed mode test with cert-manager addon")

				addonTest := &suite.AddonEc2Test{PeeredVPCTest: test}

				certManagerTest, err := addonTest.NewCertManagerTest(ctx)
				Expect(err).NotTo(HaveOccurred(), "cert-manager test should be created successfully")

				DeferCleanup(func(ctx context.Context) {
					Expect(certManagerTest.Delete(ctx)).To(Succeed(), "should cleanup cert-manager addon successfully")
				})

				DeferCleanup(func(ctx context.Context) {
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := certManagerTest.PrintLogs(ctx)
						if err != nil {
							GinkgoWriter.Printf("Failed to get cert-manager logs: %v\n", err)
						}
					}
				})

				test.Logger.Info("Created cert-manager test instance")
				err = certManagerTest.Create(ctx)
				Expect(err).NotTo(HaveOccurred(), "cert-manager addon should be created successfully")

				test.Logger.Info("cert-manager addon created and ready")

				err = certManagerTest.Validate(ctx)
				Expect(err).NotTo(HaveOccurred(), "cert-manager should validate certificates in mixed mode")

				test.Logger.Info("Cert-manager mixed mode test successful ")
			})
		})

		Context("Metrics Server Mixed Mode", func() {
			It("should support metrics server functionality in mixed mode", func(ctx context.Context) {
				testCaseLabels["test-case"] = "metricsserver-mixed-mode"

				test.Logger.Info("Starting mixed mode test with Metrics Server addon")

				addonTest := &suite.AddonEc2Test{PeeredVPCTest: test}

				metricsServerTest := addonTest.NewMetricsServerTest()

				DeferCleanup(func(ctx context.Context) {
					Expect(metricsServerTest.Delete(ctx)).To(Succeed(), "should cleanup metrics-server addon successfully")
				})

				DeferCleanup(func(ctx context.Context) {
					report := CurrentSpecReport()
					if report.State.Is(types.SpecStateFailed) {
						err := metricsServerTest.PrintLogs(ctx)
						if err != nil {
							GinkgoWriter.Printf("Failed to get metrics-server logs: %v\n", err)
						}
					}
				})

				test.Logger.Info("Created metrics server test instance")

				err := metricsServerTest.Create(ctx)
				Expect(err).NotTo(HaveOccurred(), "metrics server addon should be created successfully")

				test.Logger.Info("metrics server addon created and ready")

				err = metricsServerTest.Validate(ctx)
				Expect(err).NotTo(HaveOccurred(), "metrics server should collect metrics in mixed mode")

				test.Logger.Info("Metrics server mixed mode test successful")
			})
		})
	})
})

// cleanupTestResources performs comprehensive cleanup using provided labels
func cleanupTestResources(ctx context.Context, test *suite.PeeredVPCTest, labels map[string]string) {
	// Construct label selector from provided labels map
	var labelParts []string
	for key, value := range labels {
		labelParts = append(labelParts, fmt.Sprintf("%s=%s", key, value))
	}
	labelSelector := strings.Join(labelParts, ",")

	test.Logger.Info("Starting comprehensive cleanup using label", "selector", labelSelector)

	// Clean up pods
	err := kubernetes.DeletePodsWithLabels(ctx, test.K8sClient.Interface, testNamespace, labelSelector, test.Logger)
	if err != nil {
		test.Logger.Info("Failed to delete pods with selector", "selector", labelSelector, "error", err.Error())
	}

	// Clean up services
	err = kubernetes.DeleteServicesWithLabels(ctx, test.K8sClient.Interface, testNamespace, labelSelector, test.Logger)
	if err != nil {
		test.Logger.Info("Failed to delete services with selector", "selector", labelSelector, "error", err.Error())
	}

	// Clean up deployments
	err = kubernetes.DeleteDeploymentsWithLabels(ctx, test.K8sClient.Interface, testNamespace, labelSelector, test.Logger)
	if err != nil {
		test.Logger.Info("Failed to delete deployments with selector", "selector", labelSelector, "error", err.Error())
	}

	test.Logger.Info("Comprehensive cleanup completed", "selector", labelSelector)
}

// ensureMixedModeConnectivity adds required security group rules for mixed mode testing
func ensureMixedModeConnectivity(ctx context.Context, test *suite.PeeredVPCTest, hybridNodeName string) error {
	clusterSG, hybridSG, err := getSecurityGroupsForMixedMode(ctx, test, hybridNodeName)
	if err != nil {
		return fmt.Errorf("failed to get security groups: %w", err)
	}

	rules := []struct {
		protocol string
		port     int32
		cidr     string
		desc     string
	}{
		// HTTP connectivity for pod-to-pod and service tests
		{"tcp", httpPort, cloudVPCCIDR, "HTTP 80 from cloud VPC"},
		{"tcp", httpPort, podCIDR, "HTTP 80 from pod CIDR"},
		{"tcp", httpPort, hybridVPCCIDR, "HTTP 80 from hybrid subnet"},
		// DNS connectivity for cross-VPC CoreDNS resolution
		{"tcp", dnsPort, cloudVPCCIDR, "DNS TCP from cloud VPC"},
		{"udp", dnsPort, cloudVPCCIDR, "DNS UDP from cloud VPC"},
		{"tcp", dnsPort, podCIDR, "DNS TCP from pod CIDR"},
		{"udp", dnsPort, podCIDR, "DNS UDP from pod CIDR"},
		{"tcp", dnsPort, hybridVPCCIDR, "DNS TCP from hybrid subnet"},
		{"udp", dnsPort, hybridVPCCIDR, "DNS UDP from hybrid subnet"},
		// HTTPS webhook connectivity for CloudWatch admission controllers
		{"tcp", webhookPort, cloudVPCCIDR, "HTTPS webhook from cloud VPC"},
		{"tcp", webhookPort, podCIDR, "HTTPS webhook from pod CIDR"},
		{"tcp", webhookPort, hybridVPCCIDR, "HTTPS webhook from hybrid subnet"},
	}

	// Apply rules to both cluster security group and hybrid node security group
	securityGroups := []*string{clusterSG, hybridSG}

	for _, sg := range securityGroups {
		test.Logger.Info("Adding security group rules", "sgId", *sg)
		for _, rule := range rules {
			ipPermission := &ec2v2types.IpPermission{
				IpProtocol: &rule.protocol,
				FromPort:   &rule.port,
				ToPort:     &rule.port,
				IpRanges: []ec2v2types.IpRange{
					{CidrIp: &rule.cidr},
				},
			}
			_, err := test.EC2Client.AuthorizeSecurityGroupIngress(ctx, &ec2v2.AuthorizeSecurityGroupIngressInput{
				GroupId:       sg,
				IpPermissions: []ec2v2types.IpPermission{*ipPermission},
			})

			// Ignore "already exists" errors since rules might already be in place
			if err != nil && !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
				return fmt.Errorf("failed to add %s rule for %s on SG %s: %w", rule.protocol, rule.cidr, *sg, err)
			}
		}
	}

	if err := ensureCoreDNSDistribution(ctx, test); err != nil {
		test.Logger.Info("CoreDNS distribution configuration had issues - mixed mode will still work", "error", err.Error())
	} else {
		test.Logger.Info("CoreDNS distribution configured for optimal mixed mode performance")
	}

	// Verify CoreDNS pods are distributed across node types
	if err := kubernetes.VerifyCoreDNSDistribution(ctx, test.K8sClient.Interface, 10*time.Minute, kubernetes.MaxCoreDNSRedistributionAttempts, test.Logger); err != nil {
		test.Logger.Info("CoreDNS distribution verification had issues", "error", err.Error())
	}

	return nil
}

// getSecurityGroupsForMixedMode returns both cluster and hybrid node security groups
func getSecurityGroupsForMixedMode(ctx context.Context, test *suite.PeeredVPCTest, hybridNodeName string) (*string, *string, error) {
	clusterName := test.Cluster.Name
	cluster, err := test.EKSClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: &clusterName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to describe cluster: %w", err)
	}

	clusterSG := cluster.Cluster.ResourcesVpcConfig.ClusterSecurityGroupId
	if clusterSG == nil {
		return nil, nil, fmt.Errorf("cluster security group ID not found")
	}

	test.Logger.Info("Found EKS cluster security group", "sgId", *clusterSG)

	hybridSG, err := findHybridNodeSecurityGroup(ctx, test, hybridNodeName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find hybrid node security group: %w", err)
	}

	test.Logger.Info("Found hybrid node security group", "sgId", hybridSG)
	return clusterSG, &hybridSG, nil
}

// findHybridNodeSecurityGroup finds the security group of the hybrid node
func findHybridNodeSecurityGroup(ctx context.Context, test *suite.PeeredVPCTest, hybridNodeName string) (string, error) {
	if hybridNodeName == "" {
		return "", fmt.Errorf("hybridNodeName parameter is empty")
	}

	// Find hybrid node instance by the unique name tag
	instances, err := test.EC2Client.DescribeInstances(ctx, &ec2v2.DescribeInstancesInput{
		Filters: []ec2v2types.Filter{
			{
				Name:   &[]string{"tag:Name"}[0],
				Values: []string{hybridNodeName},
			},
			{
				Name:   &[]string{"instance-state-name"}[0],
				Values: []string{"running"},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe instances: %w", err)
	}

	if len(instances.Reservations) == 0 || len(instances.Reservations[0].Instances) == 0 {
		return "", fmt.Errorf("no hybrid node instance found with name '%s'", hybridNodeName)
	}

	instance := instances.Reservations[0].Instances[0]
	if len(instance.SecurityGroups) == 0 {
		return "", fmt.Errorf("no security groups found for hybrid node instance %s", *instance.InstanceId)
	}

	test.Logger.Info("Found hybrid node instance", "name", hybridNodeName, "instanceId", *instance.InstanceId)
	return *instance.SecurityGroups[0].GroupId, nil
}

// ensureCoreDNSDistribution configures CoreDNS for optimal mixed mode distribution using AWS best practices
func ensureCoreDNSDistribution(ctx context.Context, test *suite.PeeredVPCTest) error {
	test.Logger.Info("Configuring CoreDNS distribution across each node type for mixed mode testing")

	// Step 1: Label hybrid nodes with topology.kubernetes.io/zone=onprem
	err := kubernetes.LabelHybridNodesForTopology(ctx, test.K8sClient.Interface, test.Logger)
	if err != nil {
		return fmt.Errorf("failed to label hybrid nodes: %w", err)
	}

	err = kubernetes.ConfigureCoreDNSAntiAffinity(ctx, test.K8sClient.Interface, test.Logger)
	if err != nil {
		return fmt.Errorf("failed to configure CoreDNS anti-affinity: %w", err)
	}

	err = kubernetes.ConfigureKubeDNSTrafficDistribution(ctx, test.K8sClient.Interface, test.Logger)
	if err != nil {
		return fmt.Errorf("failed to configure kube-dns traffic distribution: %w", err)
	}

	test.Logger.Info("CoreDNS configured with preferred anti-affinity, traffic distribution, and optimal pod placement")
	return nil
}
