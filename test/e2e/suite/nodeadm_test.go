//go:build e2e
// +build e2e

package suite

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/nodeadm"
	osystem "github.com/aws/eks-hybrid/test/e2e/os"
	"github.com/aws/eks-hybrid/test/e2e/peered"
)

var (
	filePath string
	suite    *suiteConfiguration
)

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = SynchronizedBeforeSuite(
	// This function only runs once, on the first process
	// Here is where we want to run the setup infra code that should only run once
	// Whatever information we want to pass from this first process to all the processes
	// needs to be serialized into a byte slice
	// In this case, we use a struct marshalled in json.
	func(ctx context.Context) []byte {
		Expect(filePath).NotTo(BeEmpty(), "filepath should be configured") // Fail the test if the filepath flag is not provided
		config, err := e2e.ReadConfig(filePath)
		Expect(err).NotTo(HaveOccurred(), "should read valid test configuration")

		logger := newLoggerForTests().Logger
		aws, err := e2e.NewAWSConfig(ctx, awsconfig.WithRegion(config.ClusterRegion))
		Expect(err).NotTo(HaveOccurred())

		infra, err := peered.Setup(ctx, logger, aws, config.ClusterName, config.Endpoint)
		Expect(err).NotTo(HaveOccurred(), "should setup e2e resources for peered test")

		skipCleanup := os.Getenv("SKIP_CLEANUP") == "true"

		// DeferCleanup is context aware, so it will behave as SynchronizedAfterSuite
		// We prefer this because it's simpler and it avoids having to share global state
		DeferCleanup(func(ctx context.Context) {
			if skipCleanup {
				logger.Info("Skipping cleanup of e2e resources stack")
				return
			}
			Expect(infra.Teardown(ctx)).To(Succeed(), "should teardown e2e resources")
		}, NodeTimeout(constants.DeferCleanupTimeout))

		suiteJson, err := yaml.Marshal(
			&suiteConfiguration{
				TestConfig:             config,
				SkipCleanup:            skipCleanup,
				CredentialsStackOutput: &infra.Credentials.StackOutput,
				RolesAnywhereCACertPEM: infra.Credentials.RolesAnywhereCA.CertPEM,
				RolesAnywhereCAKeyPEM:  infra.Credentials.RolesAnywhereCA.KeyPEM,
				PublicKey:              infra.NodesPublicSSHKey,
				JumpboxInstanceId:      infra.JumpboxInstanceId,
			},
		)
		Expect(err).NotTo(HaveOccurred(), "suite config should be marshalled successfully")

		return suiteJson
	},
	// This function runs on all processes, and it receives the data from
	// the first process (a json serialized struct)
	// The only thing that we want to do here is unmarshal the data into
	// a struct that we can make accessible from the tests. We leave the rest
	// for the per tests setup code.
	func(ctx context.Context, data []byte) {
		Expect(data).NotTo(BeEmpty(), "suite config should have provided by first process")
		suite = &suiteConfiguration{}
		Expect(yaml.Unmarshal(data, suite)).To(Succeed(), "should unmarshal suite config coming from first test process successfully")
		Expect(suite.TestConfig).NotTo(BeNil(), "test configuration should have been set")
		Expect(suite.CredentialsStackOutput).NotTo(BeNil(), "ec2 stack output should have been set")
	},
)

var _ = Describe("Hybrid Nodes", func() {
	osList := []e2e.NodeadmOS{
		osystem.NewUbuntu2004AMD(),
		osystem.NewUbuntu2004ARM(),
		osystem.NewUbuntu2004DockerSource(),
		osystem.NewUbuntu2204AMD(),
		osystem.NewUbuntu2204ARM(),
		osystem.NewUbuntu2204DockerSource(),
		osystem.NewUbuntu2404AMD(),
		osystem.NewUbuntu2404ARM(),
		osystem.NewUbuntu2404DockerSource(),
		osystem.NewAmazonLinux2023AMD(),
		osystem.NewAmazonLinux2023ARM(),
		osystem.NewRedHat8AMD(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
		osystem.NewRedHat8ARM(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
		osystem.NewRedHat9AMD(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
		osystem.NewRedHat9ARM(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
	}
	credentialProviders := []e2e.NodeadmCredentialsProvider{
		&credentials.SsmProvider{},
		&credentials.IamRolesAnywhereProvider{},
	}

	notSupported := nodeadmConfigMatchers{}

	When("using peered VPC", func() {
		var test *peeredVPCTest

		// Here is where we setup everything we need for the test. This includes
		// reading the setup output shared by the "before suite" code. This is the only place
		// that should be reading that global state, anything needed in the test code should
		// be passed down through "local" variable state. The global state should never be modified.
		BeforeEach(func(ctx context.Context) {
			Expect(suite).NotTo(BeNil(), "suite configuration should have been set")
			Expect(suite.TestConfig).NotTo(BeNil(), "test configuration should have been set")
			Expect(suite.CredentialsStackOutput).NotTo(BeNil(), "credentials stack output should have been set")

			var err error
			test, err = buildPeeredVPCTestForSuite(ctx, suite)
			Expect(err).NotTo(HaveOccurred(), "should build peered VPC test config")

			for _, provider := range credentialProviders {
				switch p := provider.(type) {
				case *credentials.SsmProvider:
					p.SSM = test.ssmClient
					p.Role = test.stackOut.SSMNodeRoleName
				case *credentials.IamRolesAnywhereProvider:
					p.RoleARN = test.stackOut.IRANodeRoleARN
					p.ProfileARN = test.stackOut.IRAProfileARN
					p.TrustAnchorARN = test.stackOut.IRATrustAnchorARN
					p.CA = test.rolesAnywhereCA
				}
			}
		})

		When("using ec2 instance as hybrid nodes", func() {
			upgradeEntries := []TableEntry{}
			initEntries := []TableEntry{}
			for _, nodeOS := range osList {
			providerLoop:
				for _, provider := range credentialProviders {
					if notSupported.matches(nodeOS.Name(), provider.Name()) {
						continue providerLoop
					}
					initEntries = append(initEntries, Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", nodeOS.Name(), string(provider.Name())), nodeOS, provider, Label(nodeOS.Name(), string(provider.Name()), "simpleflow", "init")))
					upgradeEntries = append(upgradeEntries, Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", nodeOS.Name(), string(provider.Name())), nodeOS, provider, Label(nodeOS.Name(), string(provider.Name()), "upgradeflow")))
				}
			}

			DescribeTable("Joining a node",
				func(ctx context.Context, nodeOS e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) {
					Expect(nodeOS).NotTo(BeNil())
					Expect(provider).NotTo(BeNil())

					instanceName := test.instanceName("init", nodeOS, provider)
					nodeName := "simpleflow" + "-node-" + string(provider.Name()) + "-" + nodeOS.Name()

					k8sVersion := test.cluster.KubernetesVersion
					if test.overrideNodeK8sVersion != "" {
						k8sVersion = suite.TestConfig.NodeK8sVersion
					}

					testNode := test.newTestNode(ctx, instanceName, nodeName, k8sVersion, nodeOS, provider)
					Expect(testNode.Start(ctx)).To(Succeed(), "node should start successfully")
					Expect(testNode.Verify(ctx)).To(Succeed(), "node should be fully functional")

					test.logger.Info("Testing Pod Identity add-on functionality")
					verifyPodIdentityAddon := test.newVerifyPodIdentityAddon(testNode.PeerdNode().Name)
					Expect(verifyPodIdentityAddon.Run(ctx)).To(Succeed(), "pod identity add-on should be created successfully")

					test.logger.Info("Resetting hybrid node...")
					cleanNode := test.newCleanNode(provider, testNode.PeerdNode().Name, testNode.PeerdNode().Instance.IP)
					Expect(cleanNode.Run(ctx)).To(Succeed(), "node should have been reset successfully")

					test.logger.Info("Rebooting EC2 Instance.")
					Expect(nodeadm.RebootInstance(ctx, test.remoteCommandRunner, testNode.PeerdNode().Instance.IP)).NotTo(HaveOccurred(), "EC2 Instance should have rebooted successfully")
					test.logger.Info("EC2 Instance rebooted successfully.")

					testNode.It("re-joins the cluster after reboot", func() {
						Expect(testNode.verifyNode.WaitForNodeReady(ctx)).Error().To(Succeed(), "node should have re-joined, there must be a problem with uninstall")
					})

					Expect(testNode.Verify(ctx)).To(Succeed(), "node should be fully functional")

					if test.skipCleanup {
						test.logger.Info("Skipping nodeadm uninstall from the hybrid node...")
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
					isSupport, err := kubernetes.IsPreviousVersionSupported(test.cluster.KubernetesVersion)
					Expect(err).NotTo(HaveOccurred(), "expected to get previous k8s version")
					if !isSupport {
						Skip(fmt.Sprintf("Skipping upgrade test as minimum k8s version is %s", kubernetes.MinimumVersion))
					}

					instanceName := test.instanceName("upgrade", nodeOS, provider)
					nodeName := "upgradeflow" + "-node-" + string(provider.Name()) + "-" + nodeOS.Name()

					nodeKubernetesVersion, err := kubernetes.PreviousVersion(test.cluster.KubernetesVersion)
					Expect(err).NotTo(HaveOccurred(), "expected to get previous k8s version")

					testNode := test.newTestNode(ctx, instanceName, nodeName, nodeKubernetesVersion, nodeOS, provider)
					Expect(testNode.Start(ctx)).To(Succeed(), "node should start successfully")
					Expect(testNode.Verify(ctx)).To(Succeed(), "node should be fully functional")

					Expect(test.newUpgradeNode(testNode.PeerdNode().Name, testNode.PeerdNode().Instance.IP).Run(ctx)).To(Succeed(), "node should have upgraded successfully")

					Expect(testNode.Verify(ctx)).To(Succeed(), "node should have joined the cluster successfully after nodeadm upgrade")

					if test.skipCleanup {
						test.logger.Info("Skipping nodeadm uninstall from the hybrid node...")
						return
					}
					Expect(test.newCleanNode(provider, testNode.PeerdNode().Name, testNode.PeerdNode().Instance.IP).Run(ctx)).To(
						Succeed(), "node should have been reset successfully",
					)
				},
				upgradeEntries,
			)
		})
	})
})
