package suite

import (
	"context"
	"fmt"

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
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/cluster"
	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
	"github.com/aws/eks-hybrid/test/e2e/nodeadm"
	"github.com/aws/eks-hybrid/test/e2e/peered"
	"github.com/aws/eks-hybrid/test/e2e/s3"
	"github.com/aws/eks-hybrid/test/e2e/ssm"
)

type suiteConfiguration struct {
	TestConfig             *e2e.TestConfig          `json:"testConfig"`
	SkipCleanup            bool                     `json:"skipCleanup"`
	CredentialsStackOutput *credentials.StackOutput `json:"ec2StackOutput"`
	RolesAnywhereCACertPEM []byte                   `json:"rolesAnywhereCACertPEM"`
	RolesAnywhereCAKeyPEM  []byte                   `json:"rolesAnywhereCAPrivateKeyPEM"`
	PublicKey              string                   `json:"publicKey"`
	JumpboxInstanceId      string                   `json:"jumpboxInstanceId"`
}

type peeredVPCTest struct {
	aws             aws.Config
	eksClient       *eks.Client
	ec2Client       *ec2v2.Client
	ssmClient       *ssmv2.Client
	cfnClient       *cloudformation.Client
	k8sClient       clientgo.Interface
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

	// failureMessageLogged tracks if a terminal error due to a failed gomega
	// expectation has already been registered and logged . It avoids logging
	// the same multiple times.
	failureMessageLogged bool
}

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

	aws, err := e2e.NewAWSConfig(ctx, awsconfig.WithRegion(suite.TestConfig.ClusterRegion))
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

	test.podIdentityS3Bucket, err = addon.PodIdentityBucket(ctx, test.s3Client, test.cluster.Name)
	if err != nil {
		return nil, err
	}

	// override the default fail handler to print the error message immediately
	// following the error. We override here once the logger has been initialized
	// to ensure the error message is printed after the serial log (if it happens while waiting)
	RegisterFailHandler(test.handleFailure)

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
			SSM:                 t.ssmClient,
			S3:                  t.s3Client,
			K8s:                 t.k8sClient,
			Logger:              t.logger,
			SkipDelete:          t.skipCleanup,
			ClusterName:         t.cluster.Name,
			LogsBucket:          t.logsBucket,
		},
	}
}

func (t *peeredVPCTest) newCleanNode(provider e2e.NodeadmCredentialsProvider, nodeName, nodeIP string) *nodeadm.CleanNode {
	return &nodeadm.CleanNode{
		K8s:                 t.k8sClient,
		RemoteCommandRunner: t.remoteCommandRunner,
		Verifier:            provider,
		Logger:              t.logger,
		NodeName:            nodeName,
		NodeIP:              nodeIP,
	}
}

func (t *peeredVPCTest) newUpgradeNode(nodeName, nodeIP string) *nodeadm.UpgradeNode {
	return &nodeadm.UpgradeNode{
		K8s:                 t.k8sClient,
		RemoteCommandRunner: t.remoteCommandRunner,
		Logger:              t.logger,
		NodeName:            nodeName,
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

func (t *peeredVPCTest) newVerifyPodIdentityAddon(nodeName string) *addon.VerifyPodIdentityAddon {
	return &addon.VerifyPodIdentityAddon{
		Cluster:             t.cluster.Name,
		NodeName:            nodeName,
		PodIdentityS3Bucket: t.podIdentityS3Bucket,
		K8S:                 t.k8sClient,
		EKSClient:           t.eksClient,
		IAMClient:           t.iamClient,
		S3Client:            t.s3Client,
		Logger:              t.logger,
		K8SConfig:           t.k8sClientConfig,
		Region:              t.cluster.Region,
	}
}

func (t *peeredVPCTest) newTestNode(ctx context.Context, instanceName, nodeName, k8sVersion string, os e2e.NodeadmOS, provider e2e.NodeadmCredentialsProvider) *testNode {
	return &testNode{
		ArtifactsPath:   t.artifactsPath,
		ClusterName:     t.cluster.Name,
		EC2Client:       t.ec2Client,
		FailHandler:     t.handleFailure,
		InstanceName:    instanceName,
		Logger:          t.logger,
		LoggerControl:   t.loggerControl,
		LogsBucket:      t.logsBucket,
		PeeredNode:      t.newPeeredNode(),
		NodeName:        nodeName,
		K8sClient:       t.k8sClient,
		K8sClientConfig: t.k8sClientConfig,
		K8sVersion:      k8sVersion,
		OS:              os,
		Provider:        provider,
		Region:          t.cluster.Region,
	}
}

// handleFailure is a wrapper around ginkgo.Fail that logs the error message
// immediately after it happens. It doesn't modify gomega's or ginkgo's regular
// behavior.
// We do this to help debug errors when going through the test logs.
func (t *peeredVPCTest) handleFailure(message string, callerSkip ...int) {
	skip := 0
	if len(callerSkip) > 0 {
		skip = callerSkip[0]
	}
	if !t.failureMessageLogged {
		cl := types.NewCodeLocationWithStackTrace(skip + 1)
		err := types.GinkgoError{
			Message:      message,
			CodeLocation: cl,
		}
		t.logger.Error(nil, err.Error())
		t.failureMessageLogged = true
	}
	Fail(message, skip+1)
}

func newLoggerForTests() e2e.PausableLogger {
	_, reporter := GinkgoConfiguration()
	cfg := e2e.LoggerConfig{}
	if reporter.NoColor {
		cfg.NoColor = true
	}
	return e2e.NewPausableLogger(cfg)
}
