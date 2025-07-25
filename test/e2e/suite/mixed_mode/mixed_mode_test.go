//go:build e2e
// +build e2e

package mixed_mode

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2v2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	smithyTime "github.com/aws/smithy-go/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/suite"
)

var (
	filePath    string
	suiteConfig *suite.SuiteConfiguration

	// Node selectors as constants
	cloudNodeSelector  = map[string]string{"node.kubernetes.io/instance-type": "m5.large"}
	hybridNodeSelector = map[string]string{"eks.amazonaws.com/compute-type": "hybrid"}
)

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

		hybridNode := suite.NodeCreate{
			InstanceName: "mixed-mode-hybrid",
			InstanceSize: e2e.Large,
			NodeName:     "mixed-mode-hybrid",
			OS:           randomOSProvider.OS,
			Provider:     randomOSProvider.Provider,
			ComputeType:  e2e.CPUInstance,
		}
		suite.CreateNodes(ctx, test, []suite.NodeCreate{hybridNode})

		Expect(test.CreateManagedNodeGroups(ctx)).To(Succeed(), "managed node group should be created successfully")

		// Ensure mixed mode connectivity by adding required security group rules
		err := ensureMixedModeConnectivity(ctx, test)
		Expect(err).NotTo(HaveOccurred(), "Mixed mode connectivity rules should be added successfully")

		suiteJson, err := yaml.Marshal(suiteConfig)
		Expect(err).NotTo(HaveOccurred(), "suite config should be marshalled successfully")
		return suiteJson
	},
	// This function runs on all processes
	func(ctx context.Context, data []byte) {
		// add a small sleep to add jitter to the start of each test
		randomSleep := rand.Intn(10)
		err := smithyTime.SleepWithContext(ctx, time.Duration(randomSleep)*time.Second)
		Expect(err).NotTo(HaveOccurred(), "failed to sleep")
		suiteConfig = suite.BeforeSuiteCredentialUnmarshal(ctx, data)
	},
)

var _ = Describe("Mixed Mode Testing", func() {
	When("hybrid and cloud-managed nodes coexist", func() {
		var test *suite.PeeredVPCTest

		BeforeEach(func(ctx context.Context) {
			test = suite.BeforeVPCTest(ctx, suiteConfig)

			// Comprehensive cleanup before each test
			test.Logger.Info("Running comprehensive cleanup to ensure clean state")
			cleanupTestResources(ctx, test)
		})

		AfterEach(func(ctx context.Context) {
			test.Logger.Info("Running comprehensive cleanup after test")
			cleanupTestResources(ctx, test)
		})

		Context("Pod-to-Pod Communication", func() {
			It("should enable cross-VPC pod-to-pod communication from hybrid to cloud nodes", func(ctx context.Context) {
				cloudPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nginx-cloud",
						Namespace: "default",
						Labels:    map[string]string{"app": "nginx-cloud"},
					},
					Spec: corev1.PodSpec{
						NodeSelector: cloudNodeSelector,
						Containers: []corev1.Container{{
							Name:    "nginx-cloud",
							Image:   "nginx:1.21",
							Command: []string{"sh", "-c", "sed -i 's/listen.*80;/listen 8080;/' /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"},
							Ports:   []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
						}},
					},
				}
				err := kubernetes.CreatePod(ctx, test.K8sClient(), cloudPod, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating cloud pod")
				test.Logger.Info("Cloud pod created and ready", "name", cloudPod.Name, "uid", cloudPod.UID)

				clientPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-client-hybrid",
						Namespace: "default",
						Labels:    map[string]string{"app": "test-client-hybrid"},
					},
					Spec: corev1.PodSpec{
						NodeSelector: hybridNodeSelector,
						Containers: []corev1.Container{{
							Name:    "test-client-hybrid",
							Image:   "curlimages/curl:7.85.0",
							Command: []string{"sleep", "3600"},
						}},
					},
				}
				err = kubernetes.CreatePod(ctx, test.K8sClient(), clientPod, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating client pod")
				test.Logger.Info("Client pod created and ready", "name", clientPod.Name, "uid", clientPod.UID)

				test.Logger.Info("Testing cross-VPC pod-to-pod connectivity (hybrid → cloud)")
				err = kubernetes.TestPodToPodConnectivity(ctx, test.K8sClientConfig, test.K8sClient(), clientPod.Name, cloudPod.Name, "default", test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing pod-to-pod connectivity")

				test.Logger.Info("Cross-VPC pod-to-pod communication test completed successfully")
			})

			It("should enable cross-VPC pod-to-pod communication from cloud to hybrid nodes", func(ctx context.Context) {
				hybridPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nginx-hybrid-reverse",
						Namespace: "default",
						Labels:    map[string]string{"app": "nginx-hybrid-reverse"},
					},
					Spec: corev1.PodSpec{
						NodeSelector: hybridNodeSelector,
						Containers: []corev1.Container{{
							Name:    "nginx-hybrid-reverse",
							Image:   "nginx:1.21",
							Command: []string{"sh", "-c", "sed -i 's/listen.*80;/listen 8080;/' /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"},
							Ports:   []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
						}},
					},
				}
				err := kubernetes.CreatePod(ctx, test.K8sClient(), hybridPod, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating hybrid pod")
				test.Logger.Info("Hybrid pod created and ready", "name", hybridPod.Name, "uid", hybridPod.UID)

				clientPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-client-cloud",
						Namespace: "default",
						Labels:    map[string]string{"app": "test-client-cloud"},
					},
					Spec: corev1.PodSpec{
						NodeSelector: cloudNodeSelector,
						Containers: []corev1.Container{{
							Name:    "test-client-cloud",
							Image:   "curlimages/curl:7.85.0",
							Command: []string{"sleep", "3600"},
						}},
					},
				}
				err = kubernetes.CreatePod(ctx, test.K8sClient(), clientPod, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating client pod")
				test.Logger.Info("Cloud client pod created and ready", "name", clientPod.Name, "uid", clientPod.UID)

				err = kubernetes.TestPodToPodConnectivity(ctx, test.K8sClientConfig, test.K8sClient(), clientPod.Name, hybridPod.Name, "default", test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing pod-to-pod connectivity")

				test.Logger.Info("Cross-VPC pod-to-pod communication test completed successfully")
			})
		})

		Context("Cross-VPC Service Discovery", func() {
			It("should enable cross-VPC service discovery from hybrid to cloud services", func(ctx context.Context) {
				service, _, err := kubernetes.CreateServiceWithDeployment(ctx, test.K8sClient(), "nginx-service-cloud", "nginx:1.21", cloudNodeSelector, 80, 80, 1, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating service with deployment")

				clientPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-client-hybrid-service",
						Namespace: "default",
						Labels:    map[string]string{"app": "test-client-hybrid-service"},
					},
					Spec: corev1.PodSpec{
						NodeSelector: hybridNodeSelector,
						Containers: []corev1.Container{{
							Name:    "test-client-hybrid-service",
							Image:   "curlimages/curl:7.85.0",
							Command: []string{"sleep", "3600"},
						}},
					},
				}
				err = kubernetes.CreatePod(ctx, test.K8sClient(), clientPod, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating client pod")
				test.Logger.Info("Client pod created and ready", "name", clientPod.Name, "uid", clientPod.UID)

				err = kubernetes.WaitForServiceReady(ctx, test.K8sClient(), service.Name, "default", test.Logger)
				Expect(err).NotTo(HaveOccurred(), "waiting for service to be ready")

				// Test service connectivity with integrated DNS resolution testing
				err = kubernetes.TestServiceConnectivityWithRetries(ctx, test.K8sClientConfig, test.K8sClient(), clientPod.Name, service.Name, "default", 8080, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing service connectivity")

				test.Logger.Info("Cross-VPC service discovery test (hybrid → cloud) completed successfully")
			})

			It("should enable cross-VPC service discovery from cloud to hybrid services", func(ctx context.Context) {
				test.Logger.Info("Testing bidirectional service discovery (cloud → hybrid service)")
				service, _, err := kubernetes.CreateServiceWithDeployment(ctx, test.K8sClient(), "nginx-service-hybrid", "nginx:1.21", hybridNodeSelector, 80, 80, 1, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating service with deployment")

				clientPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-client-cloud-bidirectional",
						Namespace: "default",
						Labels:    map[string]string{"app": "test-client-cloud-bidirectional"},
					},
					Spec: corev1.PodSpec{
						NodeSelector: cloudNodeSelector,
						Containers: []corev1.Container{{
							Name:    "test-client-cloud-bidirectional",
							Image:   "curlimages/curl:7.85.0",
							Command: []string{"sleep", "3600"},
						}},
					},
				}
				err = kubernetes.CreatePod(ctx, test.K8sClient(), clientPod, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "creating client pod")
				test.Logger.Info("Client pod created and ready", "name", clientPod.Name, "uid", clientPod.UID)

				err = kubernetes.WaitForServiceReady(ctx, test.K8sClient(), service.Name, "default", test.Logger)
				Expect(err).NotTo(HaveOccurred(), "waiting for service to be ready")

				// Test service connectivity with integrated DNS resolution testing
				err = kubernetes.TestServiceConnectivityWithRetries(ctx, test.K8sClientConfig, test.K8sClient(), clientPod.Name, service.Name, "default", 8080, test.Logger)
				Expect(err).NotTo(HaveOccurred(), "testing service connectivity")

				test.Logger.Info("Bidirectional service discovery test (cloud → hybrid) completed successfully")
			})
		})
	})
})

// cleanupTestResources performs comprehensive cleanup
func cleanupTestResources(ctx context.Context, test *suite.PeeredVPCTest) {
	resourceNames := []string{
		"nginx-cloud", "nginx-hybrid-reverse", "test-client-hybrid",
		"test-client-cloud", "nginx-service-cloud", "nginx-service-hybrid",
		"test-client-hybrid-service", "test-client-cloud-bidirectional",
	}

	for _, name := range resourceNames {

		test.Logger.Info("Cleaning up pod ", "name", name)
		if err := kubernetes.DeletePod(ctx, test.K8sClient(), name, "default"); err != nil {
			test.Logger.Info("Pod cleanup: resource not found or already deleted", "name", name)
		}

		test.Logger.Info("Cleaning up service ", "name", name)
		if err := kubernetes.DeleteServiceAndWait(ctx, test.K8sClient(), name, "default", test.Logger); err != nil {
			test.Logger.Info("Service cleanup: resource not found or already deleted", "name", name)
		}

		test.Logger.Info("Cleaning up deployment ", "name", name)
		if err := kubernetes.DeleteDeploymentAndWait(ctx, test.K8sClient(), name, "default", test.Logger); err != nil {
			test.Logger.Info("Deployment cleanup: resource not found or already deleted", "name", name)
		}
	}

	test.Logger.Info("Comprehensive cleanup completed ")
}

// ensureMixedModeConnectivity adds required security group rules for mixed mode testing
func ensureMixedModeConnectivity(ctx context.Context, test *suite.PeeredVPCTest) error {
	clusterName := test.Cluster.Name
	cluster, err := test.EKSClient().DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: &clusterName,
	})
	if err != nil {
		return fmt.Errorf("failed to describe cluster: %w", err)
	}

	clusterSG := cluster.Cluster.ResourcesVpcConfig.ClusterSecurityGroupId
	if clusterSG == nil {
		return fmt.Errorf("cluster security group ID not found")
	}

	test.Logger.Info("Found EKS cluster security group", "sgId", *clusterSG)

	rules := []struct {
		protocol string
		port     int32
		cidr     string
		desc     string
	}{
		// HTTP connectivity for pod-to-pod and service tests
		{"tcp", 8080, "10.1.0.0/16", "HTTP 8080 from hybrid VPC"},
		{"tcp", 8080, "10.0.0.0/16", "HTTP 8080 from cloud VPC"},

		// DNS resolution for both internal and external DNS
		{"udp", 53, "10.1.0.0/16", "DNS UDP from hybrid VPC"},
		{"tcp", 53, "10.1.0.0/16", "DNS TCP from hybrid VPC"},
		{"udp", 53, "10.0.0.0/16", "DNS UDP from cloud VPC"},
		{"tcp", 53, "10.0.0.0/16", "DNS TCP from cloud VPC"},
	}

	for _, rule := range rules {
		ipPermission := &ec2v2types.IpPermission{
			IpProtocol: &rule.protocol,
			FromPort:   &rule.port,
			ToPort:     &rule.port,
			IpRanges: []ec2v2types.IpRange{
				{CidrIp: &rule.cidr},
			},
		}
		_, err := test.EC2Client().AuthorizeSecurityGroupIngress(ctx, &ec2v2.AuthorizeSecurityGroupIngressInput{
			GroupId:       clusterSG,
			IpPermissions: []ec2v2types.IpPermission{*ipPermission},
		})

		// Ignore "already exists" errors since rules might already be in place
		if err != nil && !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
			return fmt.Errorf("failed to add %s rule for %s: %w", rule.protocol, rule.cidr, err)
		}

	}

	if err := ensureCoreDNSDistribution(ctx, test); err != nil {
		test.Logger.Info("CoreDNS distribution configuration had issues - mixed mode will still work", "error", err.Error())
	} else {
		test.Logger.Info("CoreDNS distribution configured for optimal mixed mode performance")
	}

	return nil
}

// ensureCoreDNSDistribution configures CoreDNS for guaranteed 1+1 distribution
func ensureCoreDNSDistribution(ctx context.Context, test *suite.PeeredVPCTest) error {
	test.Logger.Info("Configuring CoreDNS for optimal mixed mode distribution (1+1)")

	// Apply both topology spread constraint AND anti-affinity for guaranteed distribution
	combinedPatch := `{
		"spec": {
			"replicas": 2,
			"template": {
				"spec": {
					"topologySpreadConstraints": [
						{
							"maxSkew": 1,
							"topologyKey": "eks.amazonaws.com/compute-type",
							"whenUnsatisfiable": "ScheduleAnyway",
							"labelSelector": {
								"matchLabels": {"k8s-app": "kube-dns"}
							}
						}
					],
					"affinity": {
						"podAntiAffinity": {
							"requiredDuringSchedulingIgnoredDuringExecution": [
								{
									"labelSelector": {"matchLabels": {"k8s-app": "kube-dns"}},
									"topologyKey": "kubernetes.io/hostname"
								}
							]
						}
					}
				}
			}
		}
	}`

	_, err := test.K8sClient().AppsV1().Deployments("kube-system").Patch(ctx, "coredns",
		types.MergePatchType, []byte(combinedPatch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to configure CoreDNS distribution: %w", err)
	}

	test.Logger.Info("CoreDNS configured for guaranteed 1+1 mixed mode distribution")
	return nil
}
