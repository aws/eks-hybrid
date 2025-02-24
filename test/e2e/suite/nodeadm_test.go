//go:build e2e
// +build e2e

package suite

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	s3v2 "github.com/aws/aws-sdk-go-v2/service/s3"
	ssmv2 "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
	"github.com/aws/eks-hybrid/test/e2e/ec2"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/nodeadm"
	osystem "github.com/aws/eks-hybrid/test/e2e/os"
	"github.com/aws/eks-hybrid/test/e2e/peered"
	"github.com/aws/eks-hybrid/test/e2e/s3"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
)

const deferCleanupTimeout = 5 * time.Minute

var (
	filePath string
	suite    *suiteConfiguration
)

type suiteConfiguration struct {
	TestConfig             *TestConfig              `json:"testConfig"`
	SkipCleanup            bool                     `json:"skipCleanup"`
	CredentialsStackOutput *credentials.StackOutput `json:"ec2StackOutput"`
	RolesAnywhereCACertPEM []byte                   `json:"rolesAnywhereCACertPEM"`
	RolesAnywhereCAKeyPEM  []byte                   `json:"rolesAnywhereCAPrivateKeyPEM"`
	PublicKey              string                   `json:"publicKey"`
	JumpboxInstanceId      string                   `json:"jumpboxInstanceId"`
}

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "E2E Suite")
}

type peeredVPCTest struct {
	aws             aws.Config // TODO: move everything to aws sdk v2
	eksClient       *eks.Client
	ec2Client       *ec2v2.Client
	ssmClient       *ssmv2.Client
	cfnClient       *cloudformation.Client
	k8sClient       *clientgo.Clientset
	k8sClientConfig *rest.Config
	s3Client        *s3v2.Client
	iamClient       *iam.Client

	logger        logr.Logger
	loggerControl e2e.PausableLogger
	logsBucket    string
	artifactsPath string

	cluster         *peered.HybridCluster
	stackOut        *credentials.StackOutput
	nodeadmURLs     e2e.NodeadmURLs
	rolesAnywhereCA *credentials.Certificate

	overrideNodeK8sVersion string
	setRootPassword        bool
	skipCleanup            bool

	publicKey string

	remoteCommandRunner commands.RemoteCommandRunner

	podIdentityS3Bucket string
}

var _ = SynchronizedBeforeSuite(
	// This function only runs once, on the first process
	// Here is where we want to run the setup infra code that should only run once
	// Whatever information we want to pass from this first process to all the processes
	// needs to be serialized into a byte slice
	// In this case, we use a struct marshalled in json.
	func(ctx context.Context) []byte {
		Expect(filePath).NotTo(BeEmpty(), "filepath should be configured") // Fail the test if the filepath flag is not provided
		config, err := ReadConfig(filePath)
		Expect(err).NotTo(HaveOccurred(), "should read valid test configuration")

		logger := newLoggerForTests().Logger
		aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(config.ClusterRegion))
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
		}, NodeTimeout(deferCleanupTimeout))

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

	notSupported := nodeadmConfigMatchers{
		{
			matchOS:            osystem.IsUbuntu2004,
			matchCredsProvider: credentials.IsIAMRolesAnywhere,
		},
		{
			matchOS:            osystem.IsRHEL8,
			matchCredsProvider: credentials.IsIAMRolesAnywhere,
		},
	}

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
			for _, nodeOS := range osList {
			providerLoop:
				for _, provider := range credentialProviders {
					if notSupported.matches(nodeOS.Name(), provider.Name()) {
						continue providerLoop
					}

					DescribeTable("Joining a node",
						func(ctx context.Context, nodeOS e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) {
							Expect(nodeOS).NotTo(BeNil())
							Expect(provider).NotTo(BeNil())

							instanceName := test.instanceName("init", nodeOS, provider)

							k8sVersion := test.cluster.KubernetesVersion
							if test.overrideNodeK8sVersion != "" {
								k8sVersion = suite.TestConfig.NodeK8sVersion
							}

							peeredNode := test.newPeeredNode()
							instance, err := peeredNode.Create(ctx, &peered.NodeSpec{
								InstanceName:   instanceName,
								NodeK8sVersion: k8sVersion,
								NodeNamePrefix: "simpleflow",
								OS:             nodeOS,
								Provider:       provider,
							})
							Expect(err).NotTo(HaveOccurred(), "EC2 Instance should have been created successfully")
							DeferCleanup(func(ctx context.Context) {
								Expect(peeredNode.Cleanup(ctx, instance)).To(Succeed())
							}, NodeTimeout(deferCleanupTimeout))

							verifyNode := test.newVerifyNode(instance.IP)

							serialOutput := peered.NewSerialOutputBlockBestEffort(ctx, &peered.SerialOutputConfig{
								PeeredNode:   peeredNode,
								Instance:     instance,
								TestLogger:   test.loggerControl,
								OutputFolder: test.artifactsPath,
							})
							Expect(err).NotTo(HaveOccurred(), "should prepare serial output")
							DeferCleanup(func() {
								serialOutput.Close()
							})

							serialOutput.It("joins the cluster", func() {
								test.logger.Info("Waiting for EC2 Instance to be Running...")
								Expect(ec2.WaitForEC2InstanceRunning(ctx, test.ec2Client, instance.ID)).To(Succeed(), "EC2 Instance should have been reached Running status")
								Expect(verifyNode.WaitForNodeReady(ctx)).Error().To(
									Succeed(), "node should have joined the cluster successfully"+
										". You can access the collected node logs at: %s", peeredNode.S3LogsURL(instance.Name),
								)
							})

							Expect(verifyNode.Run(ctx)).To(Succeed(), "node should be fully functional")

							test.logger.Info("Testing Pod Identity add-on functionality")
							verifyPodIdentityAddon := test.newVerifyPodIdentityAddon(instance.IP)
							Expect(verifyPodIdentityAddon.Run(ctx)).To(Succeed(), "pod identity add-on should be created successfully")

							test.logger.Info("Resetting hybrid node...")
							cleanNode := test.newCleanNode(provider, instance.IP)
							Expect(cleanNode.Run(ctx)).To(Succeed(), "node should have been reset successfully")

							test.logger.Info("Rebooting EC2 Instance.")
							Expect(nodeadm.RebootInstance(ctx, test.remoteCommandRunner, instance.IP)).NotTo(HaveOccurred(), "EC2 Instance should have rebooted successfully")
							test.logger.Info("EC2 Instance rebooted successfully.")

							serialOutput.It("re-joins the cluster after reboot", func() {
								Expect(verifyNode.WaitForNodeReady(ctx)).Error().To(Succeed(),
									"node should have re-joined, there must be a problem with uninstall"+
										". You can access the collected node logs at: %s", peeredNode.S3LogsURL(instance.Name),
								)
							})

							Expect(verifyNode.Run(ctx)).To(Succeed(), "node should be fully functional")

							if test.skipCleanup {
								test.logger.Info("Skipping nodeadm uninstall from the hybrid node...")
								return
							}

							Expect(cleanNode.Run(ctx)).To(Succeed(), "node should have been reset successfully")
						},
						Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", nodeOS.Name(), string(provider.Name())), nodeOS, provider, Label(nodeOS.Name(), string(provider.Name()), "simpleflow", "init")),
					)

					DescribeTable("Upgrade nodeadm flow",
						func(ctx context.Context, os e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) {
							Expect(os).NotTo(BeNil())
							Expect(provider).NotTo(BeNil())

							// Skip upgrade flow for cluster with the minimum kubernetes version
							isSupport, err := kubernetes.IsPreviousVersionSupported(test.cluster.KubernetesVersion)
							Expect(err).NotTo(HaveOccurred(), "expected to get previous k8s version")
							if !isSupport {
								Skip(fmt.Sprintf("Skipping upgrade test as minimum k8s version is %s", kubernetes.MinimumVersion))
							}

							instanceName := test.instanceName("upgrade", os, provider)

							nodeKubernetesVersion, err := kubernetes.PreviousVersion(test.cluster.KubernetesVersion)
							Expect(err).NotTo(HaveOccurred(), "expected to get previous k8s version")

							peeredNode := test.newPeeredNode()
							instance, err := peeredNode.Create(ctx, &peered.NodeSpec{
								InstanceName:   instanceName,
								NodeK8sVersion: nodeKubernetesVersion,
								NodeNamePrefix: "upgradeflow",
								OS:             os,
								Provider:       provider,
							})
							Expect(err).NotTo(HaveOccurred(), "EC2 Instance should have been created successfully")
							DeferCleanup(func(ctx context.Context) {
								Expect(peeredNode.Cleanup(ctx, instance)).To(Succeed())
							}, NodeTimeout(deferCleanupTimeout))

							verifyNode := test.newVerifyNode(instance.IP)

							serialOutput := peered.NewSerialOutputBlockBestEffort(ctx, &peered.SerialOutputConfig{
								PeeredNode:   peeredNode,
								Instance:     instance,
								TestLogger:   test.loggerControl,
								OutputFolder: test.artifactsPath,
							})
							Expect(err).NotTo(HaveOccurred(), "should prepare serial output")
							DeferCleanup(func() {
								serialOutput.Close()
							})

							serialOutput.It("joins the cluster", func() {
								test.logger.Info("Waiting for EC2 Instance to be Running...")
								Expect(ec2.WaitForEC2InstanceRunning(ctx, test.ec2Client, instance.ID)).To(Succeed(), "EC2 Instance should have been reached Running status")
								Expect(verifyNode.WaitForNodeReady(ctx)).Error().To(
									Succeed(), "node should have joined the cluster successfully"+
										". You can access the collected node logs at: %s", peeredNode.S3LogsURL(instance.Name),
								)
							})

							Expect(verifyNode.Run(ctx)).To(Succeed(), "node should be fully functional")

							Expect(test.newUpgradeNode(instance.IP).Run(ctx)).To(
								Succeed(), "node should have upgraded successfully"+
									". You can access the collected node logs at: %s", peeredNode.S3LogsURL(instance.Name),
							)

							Expect(verifyNode.Run(ctx)).To(Succeed(), "node should have joined the cluster successfully after nodeadm upgrade")

							test.logger.Info("Resetting hybrid node...")
							Expect(test.newCleanNode(provider, instance.IP).Run(ctx)).To(
								Succeed(), "node should have been reset successfully",
							)
						},
						Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", nodeOS.Name(), string(provider.Name())), nodeOS, provider, Label(nodeOS.Name(), string(provider.Name()), "upgradeflow")),
					)
				}
			}
		})
	})
})

func buildPeeredVPCTestForSuite(ctx context.Context, suite *suiteConfiguration) (*peeredVPCTest, error) {
	pausableLogger := newLoggerForTests()
	test := &peeredVPCTest{
		stackOut:               suite.CredentialsStackOutput,
		logger:                 pausableLogger.Logger,
		loggerControl:          pausableLogger,
		logsBucket:             suite.TestConfig.LogsBucket,
		artifactsPath:          suite.TestConfig.ArtifactsFolder,
		overrideNodeK8sVersion: suite.TestConfig.NodeK8sVersion,
		publicKey:              suite.PublicKey,
		setRootPassword:        suite.TestConfig.SetRootPassword,
		skipCleanup:            suite.SkipCleanup,
	}

	aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(suite.TestConfig.ClusterRegion))
	if err != nil {
		return nil, err
	}

	test.aws = aws
	test.eksClient = eks.NewFromConfig(aws)
	test.ec2Client = ec2v2.NewFromConfig(aws)
	test.ssmClient = ssmv2.NewFromConfig(aws)
	test.s3Client = s3v2.NewFromConfig(aws)
	test.cfnClient = cloudformation.NewFromConfig(aws)
	test.iamClient = iam.NewFromConfig(aws)
	test.remoteCommandRunner = ssm.NewSSHOnSSMCommandRunner(test.ssmClient, suite.JumpboxInstanceId, test.logger)

	ca, err := credentials.ParseCertificate(suite.RolesAnywhereCACertPEM, suite.RolesAnywhereCAKeyPEM)
	if err != nil {
		return nil, err
	}
	test.rolesAnywhereCA = ca

	// TODO: ideally this should be an input to the tests and not just
	// assume same name/path used by the setup command.
	clientConfig, err := clientcmd.BuildConfigFromFlags("", cluster.KubeconfigPath(suite.TestConfig.ClusterName))
	if err != nil {
		return nil, err
	}
	test.k8sClientConfig = clientConfig
	test.k8sClient, err = clientgo.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	test.cluster, err = peered.GetHybridCluster(ctx, test.eksClient, test.ec2Client, suite.TestConfig.ClusterName)
	if err != nil {
		return nil, err
	}

	urls, err := s3.BuildNodeamURLs(ctx, test.s3Client, suite.TestConfig.NodeadmUrlAMD, suite.TestConfig.NodeadmUrlARM)
	if err != nil {
		return nil, err
	}
	test.nodeadmURLs = *urls

	test.podIdentityS3Bucket, err = getPodIdentityS3Bucket(ctx, test.cluster.Name, test.s3Client)
	if err != nil {
		return nil, err
	}

	return test, nil
}

func (t *peeredVPCTest) newPeeredNode() *peered.Node {
	return &peered.Node{
		NodeCreate: peered.NodeCreate{
			AWS:             t.aws,
			EC2:             t.ec2Client,
			SSM:             t.ssmClient,
			Logger:          t.logger,
			Cluster:         t.cluster,
			NodeadmURLs:     t.nodeadmURLs,
			PublicKey:       t.publicKey,
			SetRootPassword: t.setRootPassword,
		},
		NodeCleanup: peered.NodeCleanup{
			RemoteCommandRunner: t.remoteCommandRunner,
			EC2:                 t.ec2Client,
			S3:                  t.s3Client,
			K8s:                 t.k8sClient,
			Logger:              t.logger,
			SkipDelete:          t.skipCleanup,
			ClusterName:         t.cluster.Name,
			LogsBucket:          t.logsBucket,
		},
	}
}

func (t *peeredVPCTest) newVerifyNode(nodeIP string) *kubernetes.VerifyNode {
	return &kubernetes.VerifyNode{
		ClientConfig:  t.k8sClientConfig,
		K8s:           t.k8sClient,
		Logger:        t.logger,
		Region:        t.cluster.Region,
		NodeIPAddress: nodeIP,
	}
}

func (t *peeredVPCTest) newCleanNode(provider e2e.NodeadmCredentialsProvider, nodeIP string) *nodeadm.CleanNode {
	return &nodeadm.CleanNode{
		K8s:                 t.k8sClient,
		RemoteCommandRunner: t.remoteCommandRunner,
		Verifier:            provider,
		Logger:              t.logger,
		NodeIP:              nodeIP,
	}
}

func (t *peeredVPCTest) newUpgradeNode(nodeIP string) *nodeadm.UpgradeNode {
	return &nodeadm.UpgradeNode{
		K8s:                 t.k8sClient,
		RemoteCommandRunner: t.remoteCommandRunner,
		Logger:              t.logger,
		NodeIP:              nodeIP,
		TargetK8sVersion:    t.cluster.KubernetesVersion,
	}
}

func (t *peeredVPCTest) instanceName(testName string, os e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) string {
	return fmt.Sprintf("EKSHybridCI-%s-%s-%s-%s",
		testName,
		e2e.SanitizeForAWSName(t.cluster.Name),
		e2e.SanitizeForAWSName(os.Name()),
		e2e.SanitizeForAWSName(string(provider.Name())),
	)
}

func (t *peeredVPCTest) newVerifyPodIdentityAddon(nodeIP string) *addon.VerifyPodIdentityAddon {
	return &addon.VerifyPodIdentityAddon{
		Cluster:             t.cluster.Name,
		NodeIP:              nodeIP,
		PodIdentityS3Bucket: t.podIdentityS3Bucket,
		K8S:                 t.k8sClient,
		EKSClient:           t.eksClient,
		IAMClient:           t.iamClient,
		S3Client:            t.s3Client,
		Logger:              t.logger,
		K8SConfig:           t.k8sClientConfig,
	}
}

func newLoggerForTests() e2e.PausableLogger {
	_, reporter := GinkgoConfiguration()
	cfg := e2e.LoggerConfig{}
	if reporter.NoColor {
		cfg.NoColor = true
	}
	return e2e.NewPausableLogger(cfg)
}

func getPodIdentityS3Bucket(ctx context.Context, cluster string, client *s3v2.Client) (string, error) {
	listBucketsOutput, err := client.ListBuckets(ctx, &s3v2.ListBucketsInput{
		Prefix: aws.String(addon.PodIdentityS3BucketPrefix),
	})
	if err != nil {
		return "", err
	}

	for _, bucket := range listBucketsOutput.Buckets {
		getBucketTaggingOutput, err := client.GetBucketTagging(ctx, &s3v2.GetBucketTaggingInput{
			Bucket: bucket.Name,
		})
		if err != nil {
			return "", err
		}

		var foundClusterTag, foundPodIdentityTag bool
		for _, tag := range getBucketTaggingOutput.TagSet {
			if *tag.Key == constants.TestClusterTagKey && *tag.Value == cluster {
				foundClusterTag = true
			}

			if *tag.Key == addon.PodIdentityS3BucketPrefix && *tag.Value == "true" {
				foundPodIdentityTag = true
			}

			if foundClusterTag && foundPodIdentityTag {
				return *bucket.Name, nil
			}
		}
	}
	return "", fmt.Errorf("S3 bucket for pod identity not found")
}
