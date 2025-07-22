package hybrid

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/sts"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/validation"
)

func (hnp *HybridNodeProvider) ConfigureAws(ctx context.Context) error {
	// Here we are "loading" a dummy AWS config that likely will not be used for anything since we
	// load the proper SSM/IAM-RA one later on. We are just loading it so we can pass it to the
	// validator which just uses it to resolve the SSM/IAM-RA endpoint and then makes a TCP
	// connection to that endpoint.
	baseAWSConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(hnp.nodeConfig.Spec.Cluster.Region),
	)
	if err != nil {
		return fmt.Errorf("loading aws config: %w", err)
	}

	// First verify that we can access the right AWS API endpoints for the credential provider before we try to use any of them
	if err := hnp.runner.Run(ctx, hnp.nodeConfig, creds.Validations(baseAWSConfig, hnp.nodeConfig)...); err != nil {
		return err
	}

	if hnp.nodeConfig.IsSSM() {
		configurator := SSMAWSConfigurator{
			Manager: hnp.daemonManager,
			Logger:  hnp.logger,
		}
		if err := configurator.Configure(ctx, hnp.nodeConfig); err != nil {
			return fmt.Errorf("configuring aws credentials with SSM: %w", err)
		}

		configCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		hnp.logger.Info("Waiting for AWS config to be available")
		awsConfig, err := ssm.WaitForAWSConfig(configCtx, hnp.nodeConfig, 2*time.Second)
		if err != nil {
			return fmt.Errorf("reading aws config for SSM: %w", err)
		}

		hnp.awsConfig = &awsConfig
	} else {
		configurator := RolesAnywhereAWSConfigurator{
			Manager: hnp.daemonManager,
			Logger:  hnp.logger,
		}
		if err := configurator.Configure(ctx, hnp.nodeConfig); err != nil {
			return fmt.Errorf("configuring aws credentials with IAM Roles Anywhere: %w", err)
		}

		awsConfig, err := LoadAWSConfigForRolesAnywhere(ctx, hnp.nodeConfig)
		if err != nil {
			return fmt.Errorf("generating aws config for IAM Roles Anywhere: %w", err)
		}

		hnp.awsConfig = &awsConfig
	}

	// Then verify that we can authenticate with AWS
	if err := hnp.runner.Run(ctx, hnp.nodeConfig,
		validation.New("aws-auth", sts.NewAuthenticationValidator(*hnp.awsConfig).Run),
	); err != nil {
		return err
	}

	return nil
}

func (hnp *HybridNodeProvider) GetConfig() *aws.Config {
	return hnp.awsConfig
}

type SSMAWSConfigurator struct {
	Manager daemon.DaemonManager
	Logger  *zap.Logger
}

func (c SSMAWSConfigurator) Configure(ctx context.Context, nodeConfig *api.NodeConfig) error {
	ssmDaemon := ssm.NewSsmDaemon(c.Manager, nodeConfig, c.Logger)
	if err := ssmDaemon.Configure(ctx); err != nil {
		return err
	}
	if err := ssmDaemon.EnsureRunning(ctx); err != nil {
		return err
	}
	if err := ssmDaemon.PostLaunch(); err != nil {
		return err
	}

	return nil
}

type RolesAnywhereAWSConfigurator struct {
	Manager daemon.DaemonManager
	Logger  *zap.Logger
}

func (c RolesAnywhereAWSConfigurator) Configure(ctx context.Context, nodeConfig *api.NodeConfig) error {
	if err := iamrolesanywhere.WriteAWSConfig(iamrolesanywhere.AWSConfig{
		TrustAnchorARN:       nodeConfig.Spec.Hybrid.IAMRolesAnywhere.TrustAnchorARN,
		ProfileARN:           nodeConfig.Spec.Hybrid.IAMRolesAnywhere.ProfileARN,
		RoleARN:              nodeConfig.Spec.Hybrid.IAMRolesAnywhere.RoleARN,
		Region:               nodeConfig.Spec.Cluster.Region,
		NodeName:             nodeConfig.Status.Hybrid.NodeName,
		ConfigPath:           nodeConfig.Spec.Hybrid.IAMRolesAnywhere.AwsConfigPath,
		SigningHelperBinPath: iamrolesanywhere.SigningHelperBinPath,
		CertificatePath:      nodeConfig.Spec.Hybrid.IAMRolesAnywhere.CertificatePath,
		PrivateKeyPath:       nodeConfig.Spec.Hybrid.IAMRolesAnywhere.PrivateKeyPath,
	}); err != nil {
		return err
	}

	if !nodeConfig.Spec.Hybrid.EnableCredentialsFile {
		return nil
	}

	c.Logger.Info("Configuring aws_signing_helper_update daemon")
	signingHelper := iamrolesanywhere.NewSigningHelperDaemon(c.Manager, nodeConfig, c.Logger)
	if err := signingHelper.Configure(ctx); err != nil {
		return err
	}
	if err := signingHelper.EnsureRunning(ctx); err != nil {
		return err
	}
	if err := signingHelper.PostLaunch(); err != nil {
		return err
	}

	return nil
}

func LoadAWSConfigForRolesAnywhere(ctx context.Context, nodeConfig *api.NodeConfig) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx,
		config.WithRegion(nodeConfig.Spec.Cluster.Region),
		config.WithSharedConfigFiles([]string{nodeConfig.Spec.Hybrid.IAMRolesAnywhere.AwsConfigPath}),
		config.WithSharedCredentialsFiles([]string{iamrolesanywhere.EksHybridAwsCredentialsPath}),
		config.WithSharedConfigProfile(iamrolesanywhere.ProfileName),
		// This is helpful if the machine happens to be running on an EC2 instance
		// so we avoid defaulting to IMDS by mistake.
		config.WithEC2IMDSClientEnableState(imds.ClientDisabled),
	)
}

// BuildKubeClient builds a kubernetes client from the kubelet kubeconfig
// but with the iam-ra credentials file set
// if the node is running the iam-ra service, this will avoid starting a new session
// to make the kuberenetes api calls
// if the node is not running the iam-ra service, aws config will fallback to the default
// aws_config file, which either be a creds file created by ssm or if using iam-ra, will
// exec the aws_signing_helper
func BuildKubeClient() (kubernetes.Interface, error) {
	return kubelet.GetKubeClientFromKubeConfig(kubelet.WithAwsEnvironmentVariables(map[string]string{
		"AWS_SHARED_CREDENTIALS_FILE": iamrolesanywhere.EksHybridAwsCredentialsPath,
	}))
}
