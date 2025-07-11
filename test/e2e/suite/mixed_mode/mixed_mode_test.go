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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/aws/eks-hybrid/test/e2e"
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
				cloudPod := createPodWithSelector(ctx, test, "nginx-cloud", "nginx:1.21", cloudNodeSelector, 8080)
				test.Logger.Info("Cloud pod created", "name", cloudPod.Name, "uid", cloudPod.UID)

				clientPod := createPodWithSelector(ctx, test, "test-client-hybrid", "curlimages/curl:7.85.0", hybridNodeSelector, 0)
				test.Logger.Info("Client pod created", "name", clientPod.Name, "uid", clientPod.UID)

				waitForPodReadyAndVerifyPlacement(ctx, test, cloudPod.Name, "cloud")
				waitForPodReadyAndVerifyPlacement(ctx, test, clientPod.Name, "hybrid")

				test.Logger.Info("Testing cross-VPC pod-to-pod connectivity (hybrid → cloud)")
				testPodToPodConnectivity(ctx, test, clientPod.Name, cloudPod.Name)

				test.Logger.Info("Cross-VPC pod-to-pod communication test completed successfully")
			})

			It("should enable cross-VPC pod-to-pod communication from cloud to hybrid nodes", func(ctx context.Context) {
				hybridPod := createPodWithSelector(ctx, test, "nginx-hybrid-reverse", "nginx:1.21", hybridNodeSelector, 8080)
				test.Logger.Info("Hybrid pod created", "name", hybridPod.Name, "uid", hybridPod.UID)

				clientPod := createPodWithSelector(ctx, test, "test-client-cloud", "curlimages/curl:7.85.0", cloudNodeSelector, 0)
				test.Logger.Info("Cloud client pod created", "name", clientPod.Name, "uid", clientPod.UID)

				waitForPodReadyAndVerifyPlacement(ctx, test, hybridPod.Name, "hybrid")
				waitForPodReadyAndVerifyPlacement(ctx, test, clientPod.Name, "cloud")

				testPodToPodConnectivity(ctx, test, clientPod.Name, hybridPod.Name)

				test.Logger.Info("Cross-VPC pod-to-pod communication test completed successfully")
			})
		})

		Context("Cross-VPC Service Discovery", func() {
			It("should enable cross-VPC service discovery from hybrid to cloud services", func(ctx context.Context) {
				service, _ := createServiceWithDeployment(ctx, test, "nginx-service-cloud", "nginx:1.21", cloudNodeSelector, 80, 80, 1)
				clientPod := createPodWithSelector(ctx, test, "test-client-hybrid-service", "curlimages/curl:7.85.0", hybridNodeSelector, 0)

				waitForServiceReady(ctx, test, service.Name)
				waitForPodReadyAndVerifyPlacement(ctx, test, clientPod.Name, "hybrid")

				// Test service connectivity with integrated DNS resolution testing
				testServiceConnectivityWithRetries(ctx, test, clientPod.Name, service.Name, 8080)

				test.Logger.Info("Cross-VPC service discovery test (hybrid → cloud) completed successfully")
			})

			It("should enable cross-VPC service discovery from cloud to hybrid services", func(ctx context.Context) {
				test.Logger.Info("Testing bidirectional service discovery (cloud → hybrid service)")
				service, _ := createServiceWithDeployment(ctx, test, "nginx-service-hybrid", "nginx:1.21", hybridNodeSelector, 80, 80, 1)
				clientPod := createPodWithSelector(ctx, test, "test-client-cloud-bidirectional", "curlimages/curl:7.85.0", cloudNodeSelector, 0)

				waitForServiceReady(ctx, test, service.Name)
				waitForPodReadyAndVerifyPlacement(ctx, test, clientPod.Name, "cloud")

				// Test service connectivity with integrated DNS resolution testing
				testServiceConnectivityWithRetries(ctx, test, clientPod.Name, service.Name, 8080)

				test.Logger.Info("Bidirectional service discovery test (cloud → hybrid) completed successfully")
			})
		})
	})
})

// createPodWithSelector creates a pod with the specified configuration
func createPodWithSelector(ctx context.Context, test *suite.PeeredVPCTest, name, image string, nodeSelector map[string]string, port int32) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.PodSpec{
			NodeSelector: nodeSelector,
			Containers: []corev1.Container{
				{
					Name:  name,
					Image: image,
				},
			},
		},
	}

	if port > 0 {
		pod.Spec.Containers[0].Ports = []corev1.ContainerPort{
			{ContainerPort: port, Protocol: corev1.ProtocolTCP},
		}
	}

	if strings.Contains(image, "curl") {
		pod.Spec.Containers[0].Command = []string{"sleep", "3600"}
	}

	if strings.Contains(image, "nginx") {
		pod.Spec.Containers[0].Command = []string{"sh", "-c", "sed -i 's/listen.*80;/listen 8080;/' /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"}
		pod.Spec.Containers[0].Ports = []corev1.ContainerPort{
			{ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
		}
	}

	createdPod, err := test.GetK8sClient().CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("creating pod %s", name))

	test.Logger.Info("Created pod", "name", name, "image", image)
	return createdPod
}

// createServiceWithDeployment creates a service and deployment with the specified configuration
func createServiceWithDeployment(
	ctx context.Context,
	test *suite.PeeredVPCTest,
	name, image string,
	nodeSelector map[string]string,
	servicePort, targetPort int32,
	replicas int32,
) (*corev1.Service, *appsv1.Deployment) {
	actualTargetPort := targetPort
	var containerCommand []string
	if strings.Contains(image, "nginx") {
		actualTargetPort = 8080
		containerCommand = []string{"sh", "-c", "sed -i 's/listen.*80;/listen 8080;/' /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"}
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"app": name},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					NodeSelector: nodeSelector,
					Containers: []corev1.Container{
						{
							Name:    name,
							Image:   image,
							Command: containerCommand,
							Ports: []corev1.ContainerPort{
								{ContainerPort: actualTargetPort, Protocol: corev1.ProtocolTCP},
							},
						},
					},
				},
			},
		},
	}

	createdDeployment, err := test.GetK8sClient().AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("creating deployment %s", name))

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(int(actualTargetPort)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	createdService, err := test.GetK8sClient().CoreV1().Services("default").Create(ctx, service, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("creating service %s", name))

	test.Logger.Info("Created service with deployment", "name", name, "replicas", replicas)
	return createdService, createdDeployment
}

// waitForPodReadyAndVerifyPlacement waits for pod to be ready and verifies it's on the correct node type
func waitForPodReadyAndVerifyPlacement(ctx context.Context, test *suite.PeeredVPCTest, podName, expectedNodeType string) {
	test.Logger.Info("Waiting for pod to be ready and verifying placement", "pod", podName, "expectedNodeType", expectedNodeType)

	Eventually(func() bool {
		pod, err := test.GetK8sClient().CoreV1().Pods("default").Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false
		}

		if pod.Status.Phase != corev1.PodRunning {
			return false
		}

		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}

		if !ready {
			return false
		}

		// Verify node placement
		if pod.Spec.NodeName == "" {
			return false
		}

		node, err := test.GetK8sClient().CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			return false
		}

		return verifyNodePlacement(test, node, expectedNodeType)
	}, 5*time.Minute, 10*time.Second).Should(BeTrue(), fmt.Sprintf("pod %s should be ready and placed on %s node", podName, expectedNodeType))

	test.Logger.Info("Pod ready and correctly placed", "pod", podName, "nodeType", expectedNodeType)
}

// waitForServiceReady waits for service endpoints to be ready
func waitForServiceReady(ctx context.Context, test *suite.PeeredVPCTest, serviceName string) {
	test.Logger.Info("Waiting for service endpoints", "service", serviceName)

	Eventually(func() bool {
		endpoints, err := test.GetK8sClient().CoreV1().Endpoints("default").Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return false
		}

		for _, subset := range endpoints.Subsets {
			if len(subset.Addresses) > 0 {
				return true
			}
		}
		return false
	}, 3*time.Minute, 10*time.Second).Should(BeTrue(), fmt.Sprintf("service %s should have endpoints", serviceName))

	test.Logger.Info("Service has endpoints", "service", serviceName)
}

// testServiceConnectivityWithRetries tests service connectivity using short name with 5-minute retry window
func testServiceConnectivityWithRetries(ctx context.Context, test *suite.PeeredVPCTest, clientPodName, serviceName string, port int32) {
	test.Logger.Info("Testing service connectivity with retries", "from", clientPodName, "service", serviceName, "port", port)

	serviceURL := fmt.Sprintf("http://%s:%d", serviceName, port)

	// Use Eventually with 5-minute timeout for cross-VPC service connectivity
	Eventually(func() bool {
		cmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", serviceURL, "--connect-timeout", "30", "--max-time", "60"}
		result := execInPod(ctx, test, clientPodName, cmd)
		return strings.Contains(result, "200")
	}, 5*time.Minute, 10*time.Second).Should(BeTrue(), fmt.Sprintf("Service connectivity should work from %s to %s", clientPodName, serviceName))

	test.Logger.Info("Service connectivity test completed successfully", "from", clientPodName, "to", serviceName)
}

// testPodToPodConnectivity tests direct pod-to-pod connectivity using HTTP
func testPodToPodConnectivity(ctx context.Context, test *suite.PeeredVPCTest, clientPodName, targetPodName string) {
	targetPod, err := test.GetK8sClient().CoreV1().Pods("default").Get(ctx, targetPodName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(targetPod.Status.PodIP).NotTo(BeEmpty(), fmt.Sprintf("target pod %s should have IP", targetPodName))

	targetIP := targetPod.Status.PodIP
	test.Logger.Info("Testing pod-to-pod connectivity", "from", clientPodName, "to", targetPodName, "ip", targetIP)

	// Test basic network connectivity with HTTP request to nginx on port 8080
	cmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", fmt.Sprintf("http://%s:8080", targetIP), "--connect-timeout", "10", "--max-time", "30"}

	Eventually(func() string {
		result := execInPod(ctx, test, clientPodName, cmd)
		return result
	}, 2*time.Minute, 10*time.Second).Should(ContainSubstring("200"), fmt.Sprintf("connectivity from %s to %s should work", clientPodName, targetPodName))

	test.Logger.Info("Pod-to-pod connectivity successful", "from", clientPodName, "to", targetPodName)
}

// verifyNodePlacement checks if a node matches the expected type (hybrid or cloud)
func verifyNodePlacement(test *suite.PeeredVPCTest, node *corev1.Node, expectedNodeType string) bool {
	switch expectedNodeType {
	case "hybrid":
		if nodeType, exists := node.Labels["eks.amazonaws.com/compute-type"]; exists && nodeType == "hybrid" {
			return true
		}
		if node.Name == "mixed-mode-hybrid" {
			return true
		}
		return false
	case "cloud":

		if _, exists := node.Labels["eks.amazonaws.com/nodegroup"]; exists {
			return true
		}

		if instanceType, exists := node.Labels["node.kubernetes.io/instance-type"]; exists && instanceType == "m5.large" {
			return true
		}
		return false
	default:
		return false
	}
}

// execInPod executes a command in a pod
func execInPod(ctx context.Context, test *suite.PeeredVPCTest, podName string, command []string) string {
	req := test.GetK8sClient().CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace("default").
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: command,
		Stdout:  true,
		Stderr:  true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(test.K8sClientConfig, "POST", req.URL())
	if err != nil {
		return ""
	}

	var stdout, stderr strings.Builder
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	result := stdout.String()
	if err != nil {
		test.Logger.Info("Command execution completed with error", "pod", podName, "stdout", result, "stderr", stderr.String(), "error", err.Error())
	}

	return result
}

// cleanupTestResources performs comprehensive cleanup of test resources
func cleanupTestResources(ctx context.Context, test *suite.PeeredVPCTest) {
	resourceNames := []string{
		"nginx-cloud", "nginx-hybrid-reverse", "test-client-hybrid",
		"test-client-cloud", "nginx-service-cloud", "nginx-service-hybrid",
		"test-client-hybrid-service", "test-client-cloud-bidirectional",
	}

	// Force delete each possible resource by name - pods, services, deployments
	for _, name := range resourceNames {

		if pod, err := test.GetK8sClient().CoreV1().Pods("default").Get(ctx, name, metav1.GetOptions{}); err == nil {
			test.Logger.Info("Found existing pod - force deleting", "name", name)
			deleteAndWaitForPod(ctx, test, pod.Name)
		}

		if service, err := test.GetK8sClient().CoreV1().Services("default").Get(ctx, name, metav1.GetOptions{}); err == nil {
			test.Logger.Info("Found existing service - force deleting", "name", name)
			deleteAndWaitForService(ctx, test, service.Name)
		}

		if deployment, err := test.GetK8sClient().AppsV1().Deployments("default").Get(ctx, name, metav1.GetOptions{}); err == nil {
			test.Logger.Info("Found existing deployment - force deleting", "name", name)
			deleteAndWaitForDeployment(ctx, test, deployment.Name)
		}
	}

	test.Logger.Info("Comprehensive cleanup completed")
}

// deleteAndWaitForPod deletes a pod and waits for it to be fully removed
func deleteAndWaitForPod(ctx context.Context, test *suite.PeeredVPCTest, podName string) {
	test.Logger.Info("Deleting pod and waiting for removal", "pod", podName)

	err := test.GetK8sClient().CoreV1().Pods("default").Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		test.Logger.Info("Pod deletion request failed (pod may already be gone)", "pod", podName, "error", err.Error())
		return
	}

	Eventually(func() bool {
		_, err := test.GetK8sClient().CoreV1().Pods("default").Get(ctx, podName, metav1.GetOptions{})

		return err != nil
	}, 60*time.Second, 2*time.Second).Should(BeTrue(), fmt.Sprintf("pod %s should be fully deleted", podName))

	test.Logger.Info("Pod fully deleted", "pod", podName)
}

// deleteAndWaitForService deletes a service and waits for it to be fully removed
func deleteAndWaitForService(ctx context.Context, test *suite.PeeredVPCTest, serviceName string) {
	test.Logger.Info("Deleting service and waiting for removal", "service", serviceName)

	err := test.GetK8sClient().CoreV1().Services("default").Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil {
		test.Logger.Info("Service deletion request failed (service may already be gone)", "service", serviceName, "error", err.Error())
		return
	}

	Eventually(func() bool {
		_, err := test.GetK8sClient().CoreV1().Services("default").Get(ctx, serviceName, metav1.GetOptions{})

		return err != nil
	}, 30*time.Second, 2*time.Second).Should(BeTrue(), fmt.Sprintf("service %s should be fully deleted", serviceName))

	test.Logger.Info("Service fully deleted", "service", serviceName)
}

// deleteAndWaitForDeployment deletes a deployment and waits for it to be fully removed
func deleteAndWaitForDeployment(ctx context.Context, test *suite.PeeredVPCTest, deploymentName string) {
	test.Logger.Info("Deleting deployment and waiting for removal", "deployment", deploymentName)

	err := test.GetK8sClient().AppsV1().Deployments("default").Delete(ctx, deploymentName, metav1.DeleteOptions{})
	if err != nil {
		test.Logger.Info("Deployment deletion request failed (deployment may already be gone)", "deployment", deploymentName, "error", err.Error())
		return
	}
	Eventually(func() bool {
		_, err := test.GetK8sClient().AppsV1().Deployments("default").Get(ctx, deploymentName, metav1.GetOptions{})
		return err != nil
	}, 60*time.Second, 2*time.Second).Should(BeTrue(), fmt.Sprintf("deployment %s should be fully deleted", deploymentName))

	test.Logger.Info("Deployment fully deleted", "deployment", deploymentName)
}

// ensureMixedModeConnectivity adds required security group rules for mixed mode testing
func ensureMixedModeConnectivity(ctx context.Context, test *suite.PeeredVPCTest) error {
	clusterName := test.Cluster.Name
	cluster, err := test.GetEKSClient().DescribeCluster(ctx, &eks.DescribeClusterInput{
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

	// Add each security group rule
	for _, rule := range rules {
		ipPermission := &ec2v2types.IpPermission{
			IpProtocol: &rule.protocol,
			FromPort:   &rule.port,
			ToPort:     &rule.port,
			IpRanges: []ec2v2types.IpRange{
				{CidrIp: &rule.cidr},
			},
		}
		_, err := test.GetEC2Client().AuthorizeSecurityGroupIngress(ctx, &ec2v2.AuthorizeSecurityGroupIngressInput{
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

	_, err := test.GetK8sClient().AppsV1().Deployments("kube-system").Patch(ctx, "coredns",
		types.MergePatchType, []byte(combinedPatch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to configure CoreDNS distribution: %w", err)
	}

	test.Logger.Info("CoreDNS configured for guaranteed 1+1 mixed mode distribution")
	return nil
}
