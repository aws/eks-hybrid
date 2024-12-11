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

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ssmv2 "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	ec2v1 "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
	s3v1 "github.com/aws/aws-sdk-go/service/s3"
	ssmv1 "github.com/aws/aws-sdk-go/service/ssm"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
	"github.com/aws/eks-hybrid/test/e2e/ec2"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	osystem "github.com/aws/eks-hybrid/test/e2e/os"
	"github.com/aws/eks-hybrid/test/e2e/peered"
	"github.com/aws/eks-hybrid/test/e2e/s3"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
)

const (
	ec2VolumeSize = int32(30)
	podNamespace  = "default"
)

var (
	filePath string
	suite    *suiteConfiguration
)

type TestConfig struct {
	ClusterName     string `yaml:"clusterName"`
	ClusterRegion   string `yaml:"clusterRegion"`
	NodeadmUrlAMD   string `yaml:"nodeadmUrlAMD"`
	NodeadmUrlARM   string `yaml:"nodeadmUrlARM"`
	SetRootPassword bool   `yaml:"setRootPassword"`
	NodeK8sVersion  string `yaml:"nodeK8SVersion"`
	LogsBucket      string `yaml:"logsBucket"`
}

type suiteConfiguration struct {
	TestConfig             *TestConfig              `json:"testConfig"`
	SkipCleanup            bool                     `json:"skipCleanup"`
	CredentialsStackOutput *credentials.StackOutput `json:"ec2StackOutput"`
	RolesAnywhereCACertPEM []byte                   `json:"rolesAnywhereCACertPEM"`
	RolesAnywhereCAKeyPEM  []byte                   `json:"rolesAnywhereCAPrivateKeyPEM"`
}

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "E2E Suite")
}

type peeredVPCTest struct {
	aws         awsconfig.Config // TODO: move everything to aws sdk v2
	awsSession  *session.Session
	eksClient   *eks.EKS
	ec2Client   *ec2v1.EC2
	ec2ClientV2 *ec2v2.Client
	ssmClient   *ssmv1.SSM
	ssmClientV2 *ssmv2.Client
	cfnClient   *cloudformation.CloudFormation
	k8sClient   *clientgo.Clientset
	s3Client    *s3v1.S3
	iamClient   *iam.IAM

	logger     logr.Logger
	logsBucket string

	cluster         *peered.HybridCluster
	stackOut        *credentials.StackOutput
	nodeadmURLs     e2e.NodeadmURLs
	rolesAnywhereCA *credentials.Certificate

	overrideNodeK8sVersion string
	setRootPassword        bool
	skipCleanup            bool
}

var credentialProviders = []e2e.NodeadmCredentialsProvider{&credentials.SsmProvider{}, &credentials.IamRolesAnywhereProvider{}}

var _ = SynchronizedBeforeSuite(
	// This function only runs once, on the first process
	// Here is where we want to run the setup infra code that should only run once
	// Whatever information we want to pass from this first process to all the processes
	// needs to be serialized into a byte slice
	// In this case, we use a struct marshalled in json.
	func(ctx context.Context) []byte {
		Expect(filePath).NotTo(BeEmpty(), "filepath should be configured") // Fail the test if the filepath flag is not provided
		config, err := readTestConfig(filePath)
		Expect(err).NotTo(HaveOccurred(), "should read valid test configuration")

		logger := e2e.NewLogger()
		awsSession, err := session.NewSession(&aws.Config{
			Region: aws.String(config.ClusterRegion),
		})
		Expect(err).NotTo(HaveOccurred())
		aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(config.ClusterRegion))
		Expect(err).NotTo(HaveOccurred())

		infra, err := credentials.Setup(ctx, logger, awsSession, aws, config.ClusterName)
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
		})

		suiteJson, err := yaml.Marshal(
			&suiteConfiguration{
				TestConfig:             config,
				SkipCleanup:            skipCleanup,
				CredentialsStackOutput: &infra.StackOutput,
				RolesAnywhereCACertPEM: infra.RolesAnywhereCA.CertPEM,
				RolesAnywhereCAKeyPEM:  infra.RolesAnywhereCA.KeyPEM,
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
					p.SSMv2 = test.ssmClientV2
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
			for _, os := range osList {
				for _, provider := range credentialProviders {
					DescribeTable("Joining a node",
						func(ctx context.Context, os e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) {
							Expect(os).NotTo(BeNil())
							Expect(provider).NotTo(BeNil())

							instanceName := fmt.Sprintf("EKSHybridCI-%s-%s-%s",
								e2e.SanitizeForAWSName(test.cluster.Name),
								e2e.SanitizeForAWSName(os.Name()),
								e2e.SanitizeForAWSName(string(provider.Name())),
							)

							k8sVersion := test.cluster.KubernetesVersion
							if test.overrideNodeK8sVersion != "" {
								k8sVersion = suite.TestConfig.NodeK8sVersion
							}

							createNodeTest := createNodeTest{
								awsSession:         test.awsSession,
								cluster:            test.cluster,
								ec2ClientV2:        test.ec2ClientV2,
								instanceName:       instanceName,
								instanceProfileARN: test.stackOut.InstanceProfileARN,
								k8sClient:          test.k8sClient,
								k8sVersion:         k8sVersion,
								logger:             test.logger,
								logsBucket:         test.logsBucket,
								nodeadmURLs:        test.nodeadmURLs,
								nodeNamePrefix:     "simpleflow",
								os:                 os,
								provider:           provider,
								s3Client:           test.s3Client,
								setRootPassword:    test.setRootPassword,
								skipCleanup:        test.skipCleanup,
								ssmClient:          test.ssmClient,
							}
							instance, err := createNodeTest.Run(ctx)
							Expect(err).NotTo(HaveOccurred(), "EC2 Instance should have been created successfully")

							joinNodeTest := joinNodeTest{
								k8s:           test.k8sClient,
								nodeIPAddress: instance.IP,
								logger:        test.logger,
							}
							Expect(joinNodeTest.Run(ctx)).To(Succeed(), "node should have joined the cluster successfully")

							test.logger.Info("Resetting hybrid node...")

							uninstallNodeTest := uninstallNodeTest{
								k8s:      test.k8sClient,
								ssm:      test.ssmClient,
								ec2:      instance,
								provider: provider,
								logger:   test.logger,
							}
							Expect(uninstallNodeTest.Run(ctx)).To(Succeed(), "node should have been reset successfully")

							test.logger.Info("Rebooting EC2 Instance.")
							Expect(ec2.RebootEC2Instance(ctx, test.ec2ClientV2, instance.ID)).NotTo(HaveOccurred(), "EC2 Instance should have rebooted successfully")
							test.logger.Info("EC2 Instance rebooted successfully.")

							Expect(joinNodeTest.Run(ctx)).To(Succeed(), "node should have re-joined, there must be a problem with uninstall")

							if test.skipCleanup {
								test.logger.Info("Skipping nodeadm uninstall from the hybrid node...")
								return
							}

							Expect(uninstallNodeTest.Run(ctx)).To(Succeed(), "node should have been reset successfully")
						},
						Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", os.Name(), string(provider.Name())), context.Background(), os, provider, Label(os.Name(), string(provider.Name()), "simpleflow")),
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

							instanceName := fmt.Sprintf("EKSHybridCI-upgrade-%s-%s-%s",
								e2e.SanitizeForAWSName(test.cluster.Name),
								e2e.SanitizeForAWSName(os.Name()),
								e2e.SanitizeForAWSName(string(provider.Name())),
							)

							nodeKubernetesVersion, err := kubernetes.PreviousVersion(test.cluster.KubernetesVersion)
							Expect(err).NotTo(HaveOccurred(), "expected to get previous k8s version")

							createNodeTest := createNodeTest{
								awsSession:         test.awsSession,
								cluster:            test.cluster,
								ec2ClientV2:        test.ec2ClientV2,
								instanceName:       instanceName,
								instanceProfileARN: test.stackOut.InstanceProfileARN,
								k8sClient:          test.k8sClient,
								k8sVersion:         nodeKubernetesVersion,
								logger:             test.logger,
								logsBucket:         test.logsBucket,
								nodeadmURLs:        test.nodeadmURLs,
								nodeNamePrefix:     "upgradeflow",
								os:                 os,
								provider:           provider,
								s3Client:           test.s3Client,
								skipCleanup:        test.skipCleanup,
								setRootPassword:    test.setRootPassword,
								ssmClient:          test.ssmClient,
							}
							instance, err := createNodeTest.Run(ctx)
							Expect(err).NotTo(HaveOccurred(), "EC2 Instance should have been created successfully")

							joinNodeTest := joinNodeTest{
								k8s:           test.k8sClient,
								nodeIPAddress: instance.IP,
								logger:        test.logger,
							}
							Expect(joinNodeTest.Run(ctx)).To(Succeed(), "node should have joined the cluster sucessfully")

							upgradeNodeTest := upgradeNodeTest{
								k8s:      test.k8sClient,
								ssm:      test.ssmClient,
								cluster:  test.cluster,
								ec2:      instance,
								logger:   test.logger,
								provider: provider,
							}
							Expect(upgradeNodeTest.Run(ctx)).To(Succeed(), "node should have upgraded successfully")
							Expect(joinNodeTest.Run(ctx)).To(Succeed(), "node should have joined the cluster sucessfully after nodeadm upgrade")

							test.logger.Info("Resetting hybrid node...")

							uninstallNodeTest := uninstallNodeTest{
								k8s:      test.k8sClient,
								ssm:      test.ssmClient,
								ec2:      instance,
								provider: provider,
								logger:   test.logger,
							}
							Expect(uninstallNodeTest.Run(ctx)).To(Succeed(), "node should have been reset sucessfully")
						},
						Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", os.Name(), string(provider.Name())), context.Background(), os, provider, Label(os.Name(), string(provider.Name()), "upgradeflow")),
					)
				}
			}
		})
	})
})

type createNodeTest struct {
	awsSession         *session.Session
	cluster            *peered.HybridCluster
	ec2ClientV2        *ec2v2.Client
	instanceName       string
	instanceProfileARN string
	k8sClient          *clientgo.Clientset
	k8sVersion         string
	logger             logr.Logger
	logsBucket         string
	nodeadmURLs        e2e.NodeadmURLs
	nodeNamePrefix     string
	os                 e2e.NodeadmOS
	provider           e2e.NodeadmCredentialsProvider
	s3Client           *s3v1.S3
	setRootPassword    bool
	skipCleanup        bool
	ssmClient          *ssmv1.SSM
}

func (c createNodeTest) Run(ctx context.Context) (ec2.Instance, error) {
	nodeSpec := e2e.NodeSpec{
		OS:         c.os,
		NamePrefix: c.nodeNamePrefix,
		Cluster: &e2e.Cluster{
			Name:   c.cluster.Name,
			Region: c.cluster.Region,
		},
		Provider: c.provider,
	}

	files, err := c.provider.FilesForNode(nodeSpec)
	if err != nil {
		return ec2.Instance{}, err
	}

	nodeadmConfig, err := c.provider.NodeadmConfig(ctx, nodeSpec)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to build nodeconfig: %w", err)
	}

	nodeadmConfigYaml, err := yaml.Marshal(&nodeadmConfig)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to successfully marshal nodeadm config to YAML: %w", err)
	}

	var rootPasswordHash string
	if c.setRootPassword {
		var rootPassword string
		rootPassword, rootPasswordHash, err = osystem.GenerateOSPassword()
		if err != nil {
			return ec2.Instance{}, fmt.Errorf("expected to successfully generate root password: %w", err)
		}
		c.logger.Info(fmt.Sprintf("Instance Root Password: %s", rootPassword))
	}

	var logsUploadUrls []e2e.LogsUploadUrl
	if c.logsBucket != "" {
		logsS3Prefix := fmt.Sprintf("logs/%s/%s", c.cluster.Name, c.instanceName)
		for _, name := range []string{"post-install", "post-uninstall", "post-uninstall-install", "post-final-uninstall", "post-upgrade"} {
			url, err := s3.GeneratePutLogsPreSignedURL(c.s3Client, c.logsBucket, fmt.Sprintf("%s/%s.tar.gz", logsS3Prefix, name), 30*time.Minute)
			logsUploadUrls = append(logsUploadUrls, e2e.LogsUploadUrl{Name: name, Url: url})
			if err != nil {
				return ec2.Instance{}, fmt.Errorf("expected to successfully sign logs upload path: %w", err)
			}
		}
		c.logger.Info(fmt.Sprintf("Logs bucket: https://%s.console.aws.amazon.com/s3/buckets/%s?prefix=%s/", c.cluster.Region, c.logsBucket, logsS3Prefix))
	}

	userdata, err := c.os.BuildUserData(e2e.UserDataInput{
		KubernetesVersion: c.k8sVersion,
		NodeadmUrls:       c.nodeadmURLs,
		NodeadmConfigYaml: string(nodeadmConfigYaml),
		Provider:          string(c.provider.Name()),
		RootPasswordHash:  rootPasswordHash,
		Files:             files,
		LogsUploadUrls:    logsUploadUrls,
	})
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to successfully build user data: %w", err)
	}

	amiId, err := c.os.AMIName(ctx, c.awsSession)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("expected to successfully retrieve ami id: %w", err)
	}

	ec2Input := ec2.InstanceConfig{
		ClusterName:        c.cluster.Name,
		InstanceName:       c.instanceName,
		AmiID:              amiId,
		InstanceType:       c.os.InstanceType(c.cluster.Region),
		VolumeSize:         ec2VolumeSize,
		SubnetID:           c.cluster.SubnetID,
		SecurityGroupID:    c.cluster.SecurityGroupID,
		UserData:           userdata,
		InstanceProfileARN: c.instanceProfileARN,
	}

	c.logger.Info("Creating a hybrid EC2 Instance...")
	instance, err := ec2Input.Create(ctx, c.ec2ClientV2, c.ssmClient)
	if err != nil {
		return ec2.Instance{}, fmt.Errorf("EC2 Instance should have been created successfully: %w", err)
	}
	c.logger.Info(fmt.Sprintf("EC2 Instance Connect: https://%s.console.aws.amazon.com/ec2-instance-connect/ssh?connType=serial&instanceId=%s&region=%s&serialPort=0", c.cluster.Region, instance.ID, c.cluster.Region))

	DeferCleanup(func(ctx context.Context) {
		if c.skipCleanup {
			c.logger.Info("Skipping EC2 Instance deletion", "instanceID", instance.ID)
			return
		}
		c.logger.Info("Deleting EC2 Instance", "instanceID", instance.ID)
		Expect(ec2.DeleteEC2Instance(ctx, c.ec2ClientV2, instance.ID)).NotTo(HaveOccurred(), "EC2 Instance should have been deleted successfully")
		c.logger.Info("Successfully deleted EC2 Instance", "instanceID", instance.ID)
		Expect(kubernetes.EnsureNodeWithIPIsDeleted(ctx, c.k8sClient, instance.IP)).To(Succeed(), "node should have been deleted from the cluster")
	})
	return instance, nil
}

type joinNodeTest struct {
	k8s           *clientgo.Clientset
	nodeIPAddress string
	logger        logr.Logger
}

func (t joinNodeTest) Run(ctx context.Context) error {
	// get the hybrid node registered using nodeadm by the internal IP of an EC2 Instance
	node, err := kubernetes.WaitForNode(ctx, t.k8s, t.nodeIPAddress, t.logger)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("returned node is nil")
	}

	nodeName := node.Name

	t.logger.Info("Waiting for hybrid node to be ready...")
	if err = kubernetes.WaitForHybridNodeToBeReady(ctx, t.k8s, nodeName, t.logger); err != nil {
		return err
	}

	t.logger.Info("Creating a test pod on the hybrid node...")
	podName := kubernetes.GetNginxPodName(nodeName)
	if err = kubernetes.CreateNginxPodInNode(ctx, t.k8s, nodeName, podNamespace, t.logger); err != nil {
		return err
	}
	t.logger.Info(fmt.Sprintf("Pod %s created and running on node %s", podName, nodeName))

	t.logger.Info("Deleting test pod", "pod", podName)
	if err = kubernetes.DeletePod(ctx, t.k8s, podName, podNamespace); err != nil {
		return err
	}
	t.logger.Info("Pod deleted successfully", "pod", podName)

	return nil
}

type uninstallNodeTest struct {
	k8s      *clientgo.Clientset
	ssm      *ssmv1.SSM
	ec2      ec2.Instance
	provider e2e.NodeadmCredentialsProvider
	logger   logr.Logger
}

func (u uninstallNodeTest) Run(ctx context.Context) error {
	// get the hybrid node registered using nodeadm by the internal IP of an EC2 Instance
	node, err := kubernetes.WaitForNode(ctx, u.k8s, u.ec2.IP, u.logger)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("returned node is nil")
	}

	u.logger.Info("Cordoning hybrid node...")
	err = kubernetes.CordonNode(ctx, u.k8s, node)
	if err != nil {
		return err
	}

	u.logger.Info("Draining hybrid node...")
	err = kubernetes.DrainNode(ctx, u.k8s, node)
	if err != nil {
		return err
	}

	hybridNode := e2e.HybridEC2Node{
		InstanceID: u.ec2.ID,
		Node:       *node,
	}

	if err = ssm.RunNodeadmUninstall(ctx, u.ssm, u.provider.InstanceID(hybridNode), u.logger); err != nil {
		return err
	}
	u.logger.Info("Waiting for hybrid node to be not ready...")
	if err = kubernetes.WaitForHybridNodeToBeNotReady(ctx, u.k8s, node.Name, u.logger); err != nil {
		return err
	}

	u.logger.Info("Deleting hybrid node from the cluster", "hybrid node", node.Name)
	if err = kubernetes.DeleteNode(ctx, u.k8s, node.Name); err != nil {
		return err
	}
	u.logger.Info("Node deleted successfully", "node", node.Name)

	u.logger.Info("Waiting for node to be unregistered", "node", node.Name)
	if err = u.provider.VerifyUninstall(ctx, node.Name); err != nil {
		return nil
	}
	u.logger.Info("Node unregistered successfully", "node", node.Name)

	return nil
}

// readTestConfig reads the configuration from the specified file path and unmarshals it into the TestConfig struct.
func readTestConfig(configPath string) (*TestConfig, error) {
	config := &TestConfig{}
	file, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading tests configuration file %s: %w", filePath, err)
	}

	if err = yaml.Unmarshal(file, config); err != nil {
		return nil, fmt.Errorf("unmarshaling test configuration: %w", err)
	}

	return config, nil
}

func buildPeeredVPCTestForSuite(ctx context.Context, suite *suiteConfiguration) (*peeredVPCTest, error) {
	test := &peeredVPCTest{
		stackOut:               suite.CredentialsStackOutput,
		logger:                 e2e.NewLogger(),
		logsBucket:             suite.TestConfig.LogsBucket,
		overrideNodeK8sVersion: suite.TestConfig.NodeK8sVersion,
		setRootPassword:        suite.TestConfig.SetRootPassword,
		skipCleanup:            suite.SkipCleanup,
	}

	awsSession, err := session.NewSession(&aws.Config{
		Region: aws.String(suite.TestConfig.ClusterRegion),
	})
	if err != nil {
		return nil, err
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(suite.TestConfig.ClusterRegion))
	if err != nil {
		return nil, err
	}

	test.aws = aws
	test.awsSession = awsSession
	test.eksClient = eks.New(awsSession)
	test.ec2Client = ec2v1.New(awsSession)
	test.ec2ClientV2 = ec2v2.NewFromConfig(aws) // TODO: move everything else to ec2 sdk v2
	test.ssmClient = ssmv1.New(awsSession)
	test.ssmClientV2 = ssmv2.NewFromConfig(aws)
	test.s3Client = s3v1.New(awsSession)
	test.cfnClient = cloudformation.New(awsSession)
	test.iamClient = iam.New(awsSession)

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
	test.k8sClient, err = clientgo.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	test.cluster, err = peered.GetHybridCluster(ctx, test.eksClient, test.ec2ClientV2, suite.TestConfig.ClusterName)
	if err != nil {
		return nil, err
	}

	if suite.TestConfig.NodeadmUrlAMD != "" {
		nodeadmUrl, err := s3.GetNodeadmURL(test.s3Client, suite.TestConfig.NodeadmUrlAMD)
		if err != nil {
			return nil, err
		}
		test.nodeadmURLs.AMD = nodeadmUrl
	}
	if suite.TestConfig.NodeadmUrlARM != "" {
		nodeadmUrl, err := s3.GetNodeadmURL(test.s3Client, suite.TestConfig.NodeadmUrlARM)
		if err != nil {
			return nil, err
		}
		test.nodeadmURLs.ARM = nodeadmUrl
	}
	return test, nil
}

type upgradeNodeTest struct {
	cluster  *peered.HybridCluster
	ec2      ec2.Instance
	k8s      *clientgo.Clientset
	logger   logr.Logger
	provider e2e.NodeadmCredentialsProvider
	ssm      *ssmv1.SSM
}

func (u upgradeNodeTest) Run(ctx context.Context) error {
	node, err := kubernetes.WaitForNode(ctx, u.k8s, u.ec2.IP, u.logger)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("returned node is nil")
	}
	nodeName := node.Name
	u.logger.Info("Cordoning hybrid node...")
	err = kubernetes.CordonNode(ctx, u.k8s, node)
	if err != nil {
		return err
	}

	u.logger.Info("Draining hybrid node...")
	err = kubernetes.DrainNode(ctx, u.k8s, node)
	if err != nil {
		return err
	}

	hybridNode := e2e.HybridEC2Node{
		InstanceID: u.ec2.ID,
		Node:       *node,
	}
	u.logger.Info("Upgrading hybrid node...")
	if err = ssm.RunNodeadmUpgrade(ctx, u.ssm, u.provider.InstanceID(hybridNode), u.cluster.KubernetesVersion, u.logger); err != nil {
		return err
	}

	u.logger.Info("Uncordoning hybrid node...")
	err = kubernetes.UncordonNode(ctx, u.k8s, node)
	if err != nil {
		return err
	}

	node, err = kubernetes.WaitForNodeToHaveVersion(ctx, u.k8s, node.Name, u.cluster.KubernetesVersion, u.logger)
	if err != nil {
		return err
	}

	if node.Name != nodeName {
		return fmt.Errorf("node name should not have changed during upgrade %s : %s", nodeName, node.Name)
	}

	return nil
}
