//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	ec2v2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ssmv2 "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/go-logr/logr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

const (
	ec2InstanceType = "t2.large"
	ec2VolumeSize   = int32(30)
)

var (
	filePath string
	suite    *suiteConfiguration
	ca       *certificate = &certificate{}
)

type TestConfig struct {
	ClusterName     string `yaml:"clusterName"`
	ClusterRegion   string `yaml:"clusterRegion"`
	HybridVpcID     string `yaml:"hybridVpcID"`
	NodeadmUrlAMD   string `yaml:"nodeadmUrlAMD"`
	NodeadmUrlARM   string `yaml:"nodeadmUrlARM"`
	SetRootPassword bool   `yaml:"setRootPassword"`
}

type suiteConfiguration struct {
	TestConfig             *TestConfig        `json:"testConfig"`
	EC2StackOutput         *e2eCfnStackOutput `json:"ec2StackOutput"`
	RolesAnywhereCACertPEM []byte             `json:"rolesAnywhereCACertPEM"`
	RolesAnywhereCAKeyPEM  []byte             `json:"rolesAnywhereCAPrivateKeyPEM"`
}

func init() {
	flag.StringVar(&filePath, "filepath", "", "Path to configuration")
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "E2E Suite")
}

// readTestConfig reads the configuration from the specified file path and unmarshals it into the TestConfig struct.
func readTestConfig(configPath string) (*TestConfig, error) {
	config := &TestConfig{}
	file, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading tests configuration file %s: %w", filePath, err)
	}

	if err = yaml.Unmarshal(file, config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration from YAML: %v", err)
	}

	return config, nil
}

func enabledCredentialsProviders(providers []NodeadmCredentialsProvider) []NodeadmCredentialsProvider {
	filter := GinkgoLabelFilter()
	providerList := []NodeadmCredentialsProvider{}

	for _, provider := range providers {
		if strings.Contains(filter, string(provider.Name())) {
			providerList = append(providerList, provider)
		}
	}
	return providerList
}

func getCredentialProviderNames(providers []NodeadmCredentialsProvider) string {
	var names []string
	for _, provider := range providers {
		names = append(names, string(provider.Name()))
	}
	return strings.Join(names, "-")
}

type peeredVPCTest struct {
	aws         awsconfig.Config // TODO: move everything to aws sdk v2
	awsSession  *session.Session
	eksClient   *eks.EKS
	ec2Client   *ec2.EC2
	ec2ClientV2 *ec2v2.Client
	ssmClient   *ssm.SSM
	ssmClientV2 *ssmv2.Client

	cfnClient       *cloudformation.CloudFormation
	k8sClient       *kubernetes.Clientset
	k8sClientConfig *restclient.Config

	s3Client  *s3.S3
	iamClient *iam.IAM

	logger logr.Logger

	cluster  *HybridCluster
	stackOut *e2eCfnStackOutput

	nodeadmURLs NodeadmURLs

	rolesAnywhereCA *certificate
}

func skipCleanup() bool {
	return os.Getenv("SKIP_CLEANUP") == "true"
}

var credentialProviders = []NodeadmCredentialsProvider{&SsmProvider{}, &IamRolesAnywhereProvider{}}

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

		logger := NewLogger()
		awsSession, err := newE2EAWSSession(config.ClusterRegion)
		Expect(err).NotTo(HaveOccurred())

		eksClient := eks.New(awsSession)
		ec2Client := ec2.New(awsSession)
		cfnClient := cloudformation.New(awsSession)
		iamClient := iam.New(awsSession)
		cluster, err := getHybridClusterDetails(ctx, eksClient, ec2Client, config.ClusterName, config.ClusterRegion, config.HybridVpcID)
		Expect(err).NotTo(HaveOccurred(), "expected to get cluster details")

		providerFilter := enabledCredentialsProviders(credentialProviders)
		// if there is no credential provider filter provided, then create resources for all the credential providers
		if len(providerFilter) == 0 {
			providerFilter = credentialProviders
		}

		rolesAnywhereCA, err := createCA()
		Expect(err).NotTo(HaveOccurred())

		stackName := fmt.Sprintf("EKSHybridCI-%s-%s", removeSpecialChars(config.ClusterName), getCredentialProviderNames(providerFilter))
		stack := &e2eCfnStack{
			clusterName:            cluster.clusterName,
			clusterArn:             cluster.clusterArn,
			credentialProviders:    providerFilter,
			stackName:              GetTruncatedName(stackName, 60),
			iamRolesAnywhereCACert: rolesAnywhereCA.CertPEM,
			cfn:                    cfnClient,
			iam:                    iamClient,
		}
		stackOut, err := stack.deploy(ctx, logger)
		Expect(err).NotTo(HaveOccurred(), "e2e nodes stack should have been deployed")

		// DeferCleanup is context aware, so it will behave as SynchronizedAfterSuite
		// We prefer this because it's simpler and it avoids having to share global state
		DeferCleanup(func(ctx context.Context) {
			if skipCleanup() {
				logger.Info("Skipping cleanup of e2e resources stack")
				return
			}
			logger.Info("Deleting e2e resources stack", "stackName", stack.stackName)
			Expect(stack.delete(ctx, logger, stackOut)).To(Succeed(), "should delete ec2 nodes stack successfully")
		})

		suiteJson, err := yaml.Marshal(
			&suiteConfiguration{
				TestConfig:             config,
				EC2StackOutput:         stackOut,
				RolesAnywhereCACertPEM: rolesAnywhereCA.CertPEM,
				RolesAnywhereCAKeyPEM:  rolesAnywhereCA.KeyPEM,
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
		Expect(suite.EC2StackOutput).NotTo(BeNil(), "ec2 stack output should have been set")
	},
)

var _ = Describe("Hybrid Nodes", func() {
	osList := []NodeadmOS{
		NewUbuntu2004AMD(),
		NewUbuntu2004ARM(),
		NewUbuntu2204AMD(),
		NewUbuntu2204ARM(),
		NewUbuntu2404AMD(),
		NewUbuntu2404ARM(),
		NewAmazonLinux2023AMD(),
		NewAmazonLinux2023ARM(),
		NewRedHat8AMD(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
		NewRedHat8ARM(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
		NewRedHat9AMD(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
		NewRedHat9ARM(os.Getenv("RHEL_USERNAME"), os.Getenv("RHEL_PASSWORD")),
	}

	When("using peered VPC", func() {
		skipCleanup := skipCleanup()
		var test *peeredVPCTest

		// Here is where we setup everything we need for the test. This includes
		// reading the setup output shared by the "before suite" code. This is the only place
		// that should be reading that global state, anything needed in the test code should
		// be passed down through "local" variable state. The global state should never be modified.
		BeforeEach(func(ctx context.Context) {
			Expect(suite).NotTo(BeNil(), "suite configuration should have been set")
			Expect(suite.TestConfig).NotTo(BeNil(), "test configuration should have been set")
			Expect(suite.EC2StackOutput).NotTo(BeNil(), "ec2 stack output should have been set")
			test = &peeredVPCTest{
				stackOut: suite.EC2StackOutput,
				logger:   NewLogger(),
			}

			awsSession, err := newE2EAWSSession(suite.TestConfig.ClusterRegion)
			Expect(err).NotTo(HaveOccurred())

			aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(suite.TestConfig.ClusterRegion))
			Expect(err).NotTo(HaveOccurred())

			test.aws = aws
			test.awsSession = awsSession
			test.eksClient = eks.New(awsSession)
			test.ec2Client = ec2.New(awsSession)
			test.ec2ClientV2 = ec2v2.NewFromConfig(aws) // TODO: move everything else to ec2 sdk v2
			test.ssmClient = ssm.New(awsSession)
			test.ssmClientV2 = ssmv2.NewFromConfig(aws)
			test.s3Client = s3.New(awsSession)
			test.cfnClient = cloudformation.New(awsSession)
			test.iamClient = iam.New(awsSession)

			ca, err := parseCertificate(suite.RolesAnywhereCACertPEM, suite.RolesAnywhereCAKeyPEM)
			Expect(err).NotTo(HaveOccurred())

			test.rolesAnywhereCA = ca

			// TODO: ideally this should be an input to the tests and not just
			// assume same name/path used by the setup command.
			clientConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath(suite.TestConfig.ClusterName))
			Expect(err).NotTo(HaveOccurred(), "should load correctly kubeconfig file for cluster %s", suite.TestConfig.ClusterName)
			test.k8sClientConfig = clientConfig

			test.k8sClient, err = kubernetes.NewForConfig(test.k8sClientConfig)
			Expect(err).NotTo(HaveOccurred(), "expected to build kubernetes client")

			test.cluster, err = getHybridClusterDetails(ctx, test.eksClient, test.ec2Client, suite.TestConfig.ClusterName, suite.TestConfig.ClusterRegion, suite.TestConfig.HybridVpcID)
			Expect(err).NotTo(HaveOccurred(), "expected to get cluster details")

			for _, provider := range credentialProviders {
				switch p := provider.(type) {
				case *SsmProvider:
					p.ssmClient = test.ssmClient
					p.ssmClientV2 = test.ssmClientV2
					p.role = test.stackOut.SSMNodeRoleName
				case *IamRolesAnywhereProvider:
					p.roleARN = test.stackOut.IRANodeRoleARN
					p.profileARN = test.stackOut.IRAProfileARN
					p.trustAnchorARN = test.stackOut.IRATrustAnchorARN
					p.ca = test.rolesAnywhereCA
				}
			}

			if suite.TestConfig.NodeadmUrlAMD != "" {
				nodeadmUrl, err := getNodeadmURL(test.s3Client, suite.TestConfig.NodeadmUrlAMD)
				Expect(err).NotTo(HaveOccurred(), "expected to retrieve nodeadm amd URL from S3 successfully")
				test.nodeadmURLs.AMD = nodeadmUrl
			}
			if suite.TestConfig.NodeadmUrlARM != "" {
				nodeadmUrl, err := getNodeadmURL(test.s3Client, suite.TestConfig.NodeadmUrlARM)
				Expect(err).NotTo(HaveOccurred(), "expected to retrieve nodeadm arm URL from S3 successfully")
				test.nodeadmURLs.ARM = nodeadmUrl
			}
		})

		When("using ec2 instance as hybrid nodes", func() {
			for _, os := range osList {
				for _, provider := range credentialProviders {
					DescribeTable("Joining a node",
						func(ctx context.Context, os NodeadmOS, provider NodeadmCredentialsProvider) {
							Expect(os).NotTo(BeNil())
							Expect(provider).NotTo(BeNil())

							nodeSpec := NodeSpec{
								OS:       os,
								Cluster:  test.cluster,
								Provider: provider,
							}

							instanceName := fmt.Sprintf("EKSHybridCI-%s-%s-%s",
								removeSpecialChars(test.cluster.clusterName),
								removeSpecialChars(os.Name()),
								removeSpecialChars(string(provider.Name())),
							)

							files, err := provider.FilesForNode(nodeSpec)
							Expect(err).NotTo(HaveOccurred())

							nodeadmConfig, err := provider.NodeadmConfig(ctx, nodeSpec)
							Expect(err).NotTo(HaveOccurred(), "expected to build nodeconfig")

							nodeadmConfigYaml, err := yaml.Marshal(&nodeadmConfig)
							Expect(err).NotTo(HaveOccurred(), "expected to successfully marshal nodeadm config to YAML")

							var rootPasswordHash string
							if suite.TestConfig.SetRootPassword {
								var rootPassword string
								rootPassword, rootPasswordHash, err = generateOSPassword()
								Expect(err).NotTo(HaveOccurred(), "expected to successfully generate root password")
								test.logger.Info(fmt.Sprintf("Instance Root Password: %s", rootPassword))
							}

							userdata, err := os.BuildUserData(UserDataInput{
								KubernetesVersion: test.cluster.kubernetesVersion,
								NodeadmUrls:       test.nodeadmURLs,
								NodeadmConfigYaml: string(nodeadmConfigYaml),
								Provider:          string(provider.Name()),
								RootPasswordHash:  rootPasswordHash,
								Files:             files,
							})
							Expect(err).NotTo(HaveOccurred(), "expected to successfully build user data")

							amiId, err := os.AMIName(ctx, test.awsSession)
							Expect(err).NotTo(HaveOccurred(), "expected to successfully retrieve ami id")

							ec2Input := ec2InstanceConfig{
								clusterName:        test.cluster.clusterName,
								instanceName:       instanceName,
								amiID:              amiId,
								instanceType:       os.InstanceType(),
								volumeSize:         ec2VolumeSize,
								subnetID:           test.cluster.subnetID,
								securityGroupID:    test.cluster.securityGroupID,
								userData:           userdata,
								instanceProfileARN: test.stackOut.InstanceProfileARN,
							}

							test.logger.Info("Creating a hybrid EC2 Instance...")
							ec2, err := ec2Input.create(ctx, test.ec2ClientV2, test.ssmClient)
							Expect(err).NotTo(HaveOccurred(), "EC2 Instance should have been created successfully")
							test.logger.Info(fmt.Sprintf("EC2 Instance Connect: https://%s.console.aws.amazon.com/ec2-instance-connect/ssh?connType=serial&instanceId=%s&region=%s&serialPort=0", suite.TestConfig.ClusterRegion, ec2.instanceID, suite.TestConfig.ClusterRegion))

							DeferCleanup(func(ctx context.Context) {
								if skipCleanup {
									test.logger.Info("Skipping EC2 Instance deletion", "instanceID", ec2.instanceID)
									return
								}
								test.logger.Info("Deleting EC2 Instance", "instanceID", ec2.instanceID)
								Expect(deleteEC2Instance(ctx, test.ec2ClientV2, ec2.instanceID)).NotTo(HaveOccurred(), "EC2 Instance should have been deleted successfully")
								test.logger.Info("Successfully deleted EC2 Instance", "instanceID", ec2.instanceID)
							})

							joinNodeTest := joinNodeTest{
								k8s:           test.k8sClient,
								nodeIPAddress: ec2.ipAddress,
								logger:        test.logger,
								clientConfig:  test.k8sClientConfig,
							}
							Expect(joinNodeTest.Run(ctx)).To(Succeed(), "node should have joined the cluster sucessfully")

							test.logger.Info("Resetting hybrid node...")

							uninstallNodeTest := uninstallNodeTest{
								k8s:      test.k8sClient,
								ssm:      test.ssmClient,
								ec2:      ec2,
								provider: provider,
								logger:   test.logger,
							}
							Expect(uninstallNodeTest.Run(ctx)).To(Succeed(), "node should have been reset sucessfully")

							test.logger.Info("Rebooting EC2 Instance.")
							Expect(rebootEC2Instance(ctx, test.ec2ClientV2, ec2.instanceID)).NotTo(HaveOccurred(), "EC2 Instance should have rebooted successfully")
							test.logger.Info("EC2 Instance rebooted successfully.")

							Expect(joinNodeTest.Run(ctx)).To(Succeed(), "node should have re-joined, there must be a problem with uninstall")

							if skipCleanup {
								test.logger.Info("Skipping nodeadm uninstall from the hybrid node...")
								return
							}

							Expect(uninstallNodeTest.Run(ctx)).To(Succeed(), "node should have been reset sucessfully")
						},
						Entry(fmt.Sprintf("With OS %s and with Credential Provider %s", os.Name(), string(provider.Name())), context.Background(), os, provider, Label(os.Name(), string(provider.Name()), "simpleflow")),
					)
				}
			}
		})
	})
})

type joinNodeTest struct {
	k8s           *kubernetes.Clientset
	nodeIPAddress string
	logger        logr.Logger
	clientConfig  *restclient.Config
}

func (t joinNodeTest) Run(ctx context.Context) error {
	// get the hybrid node registered using nodeadm by the internal IP of an EC2 Instance
	node, err := waitForNode(ctx, t.k8s, t.nodeIPAddress, t.logger)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("returned node is nil")
	}

	nodeName := node.Name

	t.logger.Info("Waiting for hybrid node to be ready...")
	if err = waitForHybridNodeToBeReady(ctx, t.k8s, nodeName, t.logger); err != nil {
		return err
	}

	t.logger.Info("Creating a test pod on the hybrid node...")
	podName := getNginxPodName(nodeName)
	if err = createNginxPodInNode(ctx, t.k8s, nodeName, t.logger); err != nil {
		return err
	}
	t.logger.Info(fmt.Sprintf("Pod %s created and running on node %s", podName, nodeName))

	t.logger.Info("Checking logs for nginx output", "pod", podName)
	logs, err := getPodLogs(ctx, t.k8s, podName, podNamespace)
	Expect(err).NotTo(HaveOccurred())
	Expect(logs).Should(ContainSubstring("nginx"))
	t.logger.Info("Successfully validated log output", "pod", podName)

	t.logger.Info("Exec-ing nginx -version", "pod", podName)
	stdout, stderr, err := execPod(ctx, t.clientConfig, t.k8s, podName, podNamespace, []string{"/sbin/nginx", "-version"})
	Expect(err).NotTo(HaveOccurred())
	Expect(stdout).Should(ContainSubstring("nginx"))
	Expect(stderr).Should(BeEmpty())
	t.logger.Info("Successfully exec'd nginx -version", "pod", podName)

	t.logger.Info("Deleting test pod", "pod", podName)
	if err = deletePod(ctx, t.k8s, podName, podNamespace); err != nil {
		return err
	}
	t.logger.Info("Pod deleted successfully", "pod", podName)

	return nil
}

type uninstallNodeTest struct {
	k8s      *kubernetes.Clientset
	ssm      *ssm.SSM
	ec2      ec2Instance
	provider NodeadmCredentialsProvider
	logger   logr.Logger
}

func (u uninstallNodeTest) Run(ctx context.Context) error {
	// get the hybrid node registered using nodeadm by the internal IP of an EC2 Instance
	node, err := waitForNode(ctx, u.k8s, u.ec2.ipAddress, u.logger)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("returned node is nil")
	}

	hybridNode := HybridEC2dNode{
		ec2Instance: u.ec2,
		node:        *node,
	}

	if err = runNodeadmUninstall(ctx, u.ssm, u.provider.InstanceID(hybridNode), u.logger); err != nil {
		return err
	}
	u.logger.Info("Waiting for hybrid node to be not ready...")
	if err = waitForHybridNodeToBeNotReady(ctx, u.k8s, node.Name, u.logger); err != nil {
		return err
	}

	u.logger.Info("Deleting hybrid node from the cluster", "hybrid node", node.Name)
	if err = deleteNode(ctx, u.k8s, node.Name); err != nil {
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

// removeSpecialChars removes everything except alphanumeric characters and hyphens from a string.
func removeSpecialChars(input string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9-]+`)
	return re.ReplaceAllString(input, "")
}
