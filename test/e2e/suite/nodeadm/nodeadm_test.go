//go:build e2e
// +build e2e

package nodeadm

import (
	"context"
	"flag"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/suite"
)

var (
	filePath    string
	suiteConfig *suite.SuiteConfiguration
)

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Nodeadm E2E Suite")
}

var _ = SynchronizedBeforeSuite(
	// This function only runs once, on the first process
	// Here is where we want to run the setup infra code that should only run once
	// Whatever information we want to pass from this first process to all the processes
	// needs to be serialized into a byte slice
	// In this case, we use a struct marshalled in json.
	func(ctx context.Context) []byte {
		suiteConfig := suite.BeforeSuiteCredentialSetup(ctx, filePath)
		suiteJson, err := yaml.Marshal(suiteConfig)
		Expect(err).NotTo(HaveOccurred(), "suite config should be marshalled successfully")
		return suiteJson
	},
	// This function runs on all processes, and it receives the data from
	// the first process (a json serialized struct)
	// The only thing that we want to do here is unmarshal the data into
	// a struct that we can make accessible from the tests. We leave the rest
	// for the per tests setup code.
	func(ctx context.Context, data []byte) {
		suiteConfig = suite.BeforeSuiteCredentialUnmarshal(ctx, data)
	},
)

var _ = Describe("Hybrid Nodes", func() {
	When("using peered VPC", func() {
		var test *suite.PeeredVPCTest
		credentialProviders := suite.CredentialProviders()

		// Here is where we setup everything we need for the test. This includes
		// reading the setup output shared by the "before suite" code. This is the only place
		// that should be reading that global state, anything needed in the test code should
		// be passed down through "local" variable state. The global state should never be modified.
		BeforeEach(func(ctx context.Context) {
			test = suite.BeforeVPCTest(ctx, suiteConfig)
			credentialProviders = suite.AddClientsToCredentialProviders(credentialProviders, test)
		})

		When("using ec2 instance as hybrid nodes", func() {
			upgradeEntries := []TableEntry{}
			initEntries := []TableEntry{}
			for _, osProvider := range suite.OSProviderList(credentialProviders) {
				os := osProvider.OS
				provider := osProvider.Provider
				initEntries = append(initEntries, Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", os.Name(), string(provider.Name())), os, provider, Label(os.Name(), string(provider.Name()), "simpleflow", "init")))
				upgradeEntries = append(upgradeEntries, Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", os.Name(), string(provider.Name())), os, provider, Label(os.Name(), string(provider.Name()), "upgradeflow")))
			}

			DescribeTable("Joining a node",
				func(ctx context.Context, nodeOS e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) {
					Expect(nodeOS).NotTo(BeNil())
					Expect(provider).NotTo(BeNil())

					instanceName := test.InstanceName("init", nodeOS, provider)
					nodeName := "simpleflow" + "-node-" + string(provider.Name()) + "-" + nodeOS.Name()

					k8sVersion := test.Cluster.KubernetesVersion
					if test.OverrideNodeK8sVersion != "" {
						k8sVersion = test.OverrideNodeK8sVersion
					}

					testNode := test.NewTestNode(ctx, instanceName, nodeName, k8sVersion, nodeOS, provider, e2e.Large)
					Expect(testNode.Start(ctx)).To(Succeed(), "node should start successfully")
					Expect(testNode.Verify(ctx)).To(Succeed(), "node should be fully functional")

					test.Logger.Info("Testing Pod Identity add-on functionality")
					verifyPodIdentityAddon := test.NewVerifyPodIdentityAddon(testNode.PeerdNode().Name)
					Expect(verifyPodIdentityAddon.Run(ctx)).To(Succeed(), "pod identity add-on should be created successfully")

					test.Logger.Info("Resetting hybrid node...")
					n := testNode.PeerdNode()
					cleanNode := test.NewCleanNode(
						provider,
						testNode.PeeredNode.NodeInfrastructureCleaner(*n),
						n.Name,
						n.Instance.IP,
					)
					Expect(cleanNode.Run(ctx)).To(Succeed(), "node should have been reset successfully")

					test.Logger.Info("Rebooting EC2 Instance.")
					Expect(cleanNode.RebootInstance(ctx)).NotTo(HaveOccurred(), "EC2 Instance should have rebooted successfully")
					test.Logger.Info("EC2 Instance rebooted successfully.")

					testNode.It("re-joins the cluster after reboot", func() {
						Expect(testNode.WaitForNodeReady(ctx)).Error().To(Succeed(), "node should have re-joined, there must be a problem with uninstall")
					})
					Expect(testNode.PeeredNetwork.CreateRoutesForNode(ctx, n)).Should(Succeed(), "EC2 route to pod CIDR should have been created successfully")

					Expect(testNode.Verify(ctx)).To(Succeed(), "node should be fully functional")

					if test.SkipCleanup {
						test.Logger.Info("Skipping nodeadm uninstall from the hybrid node...")
						return
					}

					Expect(cleanNode.Run(ctx)).To(Succeed(), "node should have been reset successfully")
				},
				initEntries,
			)

			DescribeTable("Upgrade nodeadm flow",
				func(ctx context.Context, nodeOS e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) {
					Expect(nodeOS).NotTo(BeNil())
					Expect(provider).NotTo(BeNil())

					// Skip upgrade flow for cluster with the minimum kubernetes version
					isSupport, err := kubernetes.IsPreviousVersionSupported(test.Cluster.KubernetesVersion)
					Expect(err).NotTo(HaveOccurred(), "expected to get previous k8s version")
					if !isSupport {
						Skip(fmt.Sprintf("Skipping upgrade test as minimum k8s version is %s", kubernetes.MinimumVersion))
					}

					instanceName := test.InstanceName("upgrade", nodeOS, provider)
					nodeName := "upgradeflow" + "-node-" + string(provider.Name()) + "-" + nodeOS.Name()

					nodeKubernetesVersion, err := kubernetes.PreviousVersion(test.Cluster.KubernetesVersion)
					Expect(err).NotTo(HaveOccurred(), "expected to get previous k8s version")

					testNode := test.NewTestNode(ctx, instanceName, nodeName, nodeKubernetesVersion, nodeOS, provider, e2e.Large)
					Expect(testNode.Start(ctx)).To(Succeed(), "node should start successfully")
					Expect(testNode.Verify(ctx)).To(Succeed(), "node should be fully functional")

					Expect(test.NewUpgradeNode(testNode.PeerdNode().Name, testNode.PeerdNode().Instance.IP).Run(ctx)).To(Succeed(), "node should have upgraded successfully")

					Expect(testNode.Verify(ctx)).To(Succeed(), "node should have joined the cluster successfully after nodeadm upgrade")

					if test.SkipCleanup {
						test.Logger.Info("Skipping nodeadm uninstall from the hybrid node...")
						return
					}

					n := testNode.PeerdNode()
					cleanNode := test.NewCleanNode(
						provider,
						testNode.PeeredNode.NodeInfrastructureCleaner(*n),
						n.Name,
						n.Instance.IP,
					)
					Expect(cleanNode.Run(ctx)).To(
						Succeed(), "node should have been reset successfully",
					)
				},
				upgradeEntries,
			)
		})
	})
})
