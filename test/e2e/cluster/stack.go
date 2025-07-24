package cluster

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfnTypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamTypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmTypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/cfn"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
	e2eCommands "github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	e2eErrors "github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/os"
	"github.com/aws/eks-hybrid/test/e2e/peered"
	"github.com/aws/eks-hybrid/test/e2e/securitygroup"
	e2eSSM "github.com/aws/eks-hybrid/test/e2e/ssm"
)

//go:embed cfn-templates/setup-cfn.yaml
var setupTemplateBody []byte

const (
	creationTimeParameterKey = "CreationTime"
	stackWaitTimeout         = 15 * time.Minute
	stackWaitInterval        = 10 * time.Second
)

type vpcConfig struct {
	vpcID         string
	publicSubnet  string
	privateSubnet string
	securityGroup string
}

type podIdentity struct {
	roleArn  string
	s3Bucket string
}

type resourcesStackOutput struct {
	clusterRole      string
	clusterVpcConfig vpcConfig
	podIdentity      podIdentity
}

type stack struct {
	logger    logr.Logger
	cfn       *cloudformation.Client
	ssmClient *ssm.Client
	ec2Client *ec2.Client
	s3Client  *s3.Client
	iamClient *iam.Client
}

func (s *stack) deploy(ctx context.Context, test TestResources) (*resourcesStackOutput, error) {
	stackName := stackName(test.ClusterName)

	params := s.prepareStackParameters(test, test.EKS)

	// There are occasional race conditions when creating the cfn stack
	// retrying once allows to potentially resolve them on the second attempt
	// avoiding the need to retry the entire test suite.
	var err error
	for range 2 {
		err = s.createOrUpdateStack(ctx, stackName, params, test)
		if err == nil {
			break
		}
		s.logger.Error(err, "Error deploying stack, retrying")
	}

	if err != nil {
		return nil, fmt.Errorf("creating or updating hybrid nodes setup cfn stack: %w", err)
	}

	// We explictly fetch the keypair/jumpbox instead of relying on outputs
	// from the cfn stack. We have seen cases across different regions
	// where the outputs have been unreliable
	if err := s.tagKeyPairSSMParameter(ctx, test.ClusterName); err != nil {
		return nil, fmt.Errorf("tagging key pair: %w", err)
	}

	if err := s.setupJumpbox(ctx, test.ClusterName); err != nil {
		return nil, fmt.Errorf("setting up jumpbox: %w", err)
	}

	resp, err := s.cfn.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	})
	if err != nil {
		return nil, fmt.Errorf("describing hybrid nodes setup cfn stack: %w", err)
	}
	result := s.processStackOutputs(resp.Stacks[0].Outputs)

	s.logger.Info("E2E resources stack deployed successfully", "stackName", stackName)
	return result, nil
}

func (s *stack) prepareStackParameters(test TestResources, eks EKSConfig) []cfnTypes.Parameter {
	params := []cfnTypes.Parameter{
		{
			ParameterKey:   aws.String("ClusterName"),
			ParameterValue: aws.String(test.ClusterName),
		},
		{
			ParameterKey:   aws.String("ClusterRegion"),
			ParameterValue: aws.String(test.ClusterRegion),
		},
		{
			ParameterKey:   aws.String("ClusterVPCCidr"),
			ParameterValue: aws.String(test.ClusterNetwork.VpcCidr),
		},
		{
			ParameterKey:   aws.String("ClusterPublicSubnetCidr"),
			ParameterValue: aws.String(test.ClusterNetwork.PublicSubnetCidr),
		},
		{
			ParameterKey:   aws.String("ClusterPrivateSubnetCidr"),
			ParameterValue: aws.String(test.ClusterNetwork.PrivateSubnetCidr),
		},
		{
			ParameterKey:   aws.String("HybridNodeVPCCidr"),
			ParameterValue: aws.String(test.HybridNetwork.VpcCidr),
		},
		{
			ParameterKey:   aws.String("HybridNodePublicSubnetCidr"),
			ParameterValue: aws.String(test.HybridNetwork.PublicSubnetCidr),
		},
		{
			ParameterKey:   aws.String("HybridNodePodCidr"),
			ParameterValue: aws.String(test.HybridNetwork.PodCidr),
		},
		{
			ParameterKey:   aws.String("TestClusterTagKey"),
			ParameterValue: aws.String(constants.TestClusterTagKey),
		},
		{
			ParameterKey:   aws.String("PodIdentityS3BucketPrefix"),
			ParameterValue: aws.String(addon.PodIdentityS3BucketPrefix),
		},
		{
			ParameterKey:   aws.String("RolePathPrefix"),
			ParameterValue: aws.String(constants.TestRolePathPrefix),
		},
		{
			// The VPC resources do not have a creation date, so we use the current time
			// and set it as a tag on the resources. This is used during cleanup to
			// determine if the resources are old enough to be deleted.
			// This date can change during an rerun of the setup which will update the stack
			// updating the stack is typically not done by tests and worst case the cleanup
			// waits a bit longer to delete a dangling resource.
			ParameterKey:   aws.String(creationTimeParameterKey),
			ParameterValue: aws.String(time.Now().Format(time.RFC3339)),
		},
		{
			ParameterKey:   aws.String("CreationTimeTagKey"),
			ParameterValue: aws.String(constants.CreationTimeTagKey),
		},
		{
			ParameterKey:   aws.String("EKSClusterRoleSP"),
			ParameterValue: aws.String(eks.ClusterRoleSP),
		},
		{
			ParameterKey:   aws.String("EKSPodIdentitySP"),
			ParameterValue: aws.String(eks.PodIdentitySP),
		},
	}

	// need to replace the ${foo} with ${!foo} otherwise cfn tries to resolve it
	// and indent the script to create a properly formated cloud-init
	logCollectorScript := strings.ReplaceAll(string(os.LogCollectorScript), "${", "${!")
	logCollectorScript = strings.ReplaceAll(logCollectorScript, "\n", "\n"+strings.Repeat(" ", 6))
	params = append(params, cfnTypes.Parameter{
		ParameterKey:   aws.String("LogCollectorScript"),
		ParameterValue: aws.String(logCollectorScript),
	})

	// Add addon parameters
	ingressConfig := securitygroup.DefaultIngress()
	params = append(params, s.prepareAddonParameters(ingressConfig)...)
	return params
}

func (s *stack) prepareAddonParameters(ingressConfig []securitygroup.IngessConfig) []cfnTypes.Parameter {
	var ports []string

	for _, cfg := range ingressConfig {
		ports = append(ports, fmt.Sprintf("%d|%s", cfg.Port, cfg.Description))
	}

	return []cfnTypes.Parameter{
		{
			ParameterKey:   aws.String("AddonPortsWithDescriptions"),
			ParameterValue: aws.String(strings.Join(ports, ",")),
		},
	}
}

func (s *stack) createOrUpdateStack(ctx context.Context, stackName string, params []cfnTypes.Parameter, test TestResources) error {
	resp, err := s.cfn.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	})
	if err != nil && !e2eErrors.IsCFNStackNotFound(err) {
		return fmt.Errorf("looking for hybrid nodes cfn stack: %w", err)
	}

	if resp == nil || resp.Stacks == nil {
		s.logger.Info("Creating hybrid nodes setup stack", "stackName", stackName)
		_, err = s.cfn.CreateStack(ctx, &cloudformation.CreateStackInput{
			DisableRollback: aws.Bool(true),
			StackName:       aws.String(stackName),
			TemplateBody:    aws.String(string(setupTemplateBody)),
			Parameters:      params,
			Capabilities: []cfnTypes.Capability{
				cfnTypes.CapabilityCapabilityIam,
				cfnTypes.CapabilityCapabilityNamedIam,
				cfnTypes.CapabilityCapabilityAutoExpand,
			},
			Tags: []cfnTypes.Tag{{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(test.ClusterName),
			}},
		})
		if err != nil {
			return fmt.Errorf("creating hybrid nodes setup cfn stack: %w", err)
		}

		s.logger.Info("Waiting for hybrid nodes setup stack to be created", "stackName", stackName)
		err = cfn.WaitForStackOperation(ctx, s.cfn, stackName, stackWaitInterval, stackWaitTimeout)
		if err != nil {
			return fmt.Errorf("waiting for hybrid nodes setup cfn stack: %w", err)
		}
	} else if resp.Stacks[0].StackStatus == cfnTypes.StackStatusCreateInProgress || resp.Stacks[0].StackStatus == cfnTypes.StackStatusUpdateInProgress {
		s.logger.Info("Found existing stack in progress, waiting for completion", "stackName", stackName, "status", resp.Stacks[0].StackStatus)
		err = cfn.WaitForStackOperation(ctx, s.cfn, stackName, stackWaitInterval, stackWaitTimeout)
		if err != nil {
			return fmt.Errorf("waiting for hybrid nodes setup cfn stack: %w", err)
		}
	} else {
		params = replaceCreationTimeParameter(resp.Stacks[0].Parameters, params)
		s.logger.Info("Updating hybrid nodes setup stack", "stackName", stackName)
		_, err = s.cfn.UpdateStack(ctx, &cloudformation.UpdateStackInput{
			DisableRollback: aws.Bool(true),
			StackName:       aws.String(stackName),
			Capabilities: []cfnTypes.Capability{
				cfnTypes.CapabilityCapabilityIam,
				cfnTypes.CapabilityCapabilityNamedIam,
				cfnTypes.CapabilityCapabilityAutoExpand,
			},
			TemplateBody: aws.String(string(setupTemplateBody)),
			Parameters:   params,
		})
		if err != nil {
			// Handle "No updates are to be performed"
			if strings.Contains(err.Error(), "No updates are to be performed") {
				s.logger.Info("No changes detected in the stack, no update necessary.")
			} else {
				return fmt.Errorf("updating CloudFormation stack: %w", err)
			}
		}

		s.logger.Info("Waiting for hybrid nodes setup stack to be updated", "stackName", stackName)
		err = cfn.WaitForStackOperation(ctx, s.cfn, stackName, stackWaitInterval, stackWaitTimeout)
		if err != nil {
			return fmt.Errorf("waiting for hybrid nodes setup cfn stack: %w", err)
		}
	}
	return nil
}

func (s *stack) processStackOutputs(outputs []cfnTypes.Output) *resourcesStackOutput {
	result := &resourcesStackOutput{}
	// extract relevant stack outputs
	for _, output := range outputs {
		switch aws.ToString(output.OutputKey) {
		case "ClusterRole":
			result.clusterRole = *output.OutputValue
		case "ClusterVPC":
			result.clusterVpcConfig.vpcID = *output.OutputValue
		case "ClusterVPCPublicSubnet":
			result.clusterVpcConfig.publicSubnet = *output.OutputValue
		case "ClusterVPCPrivateSubnet":
			result.clusterVpcConfig.privateSubnet = *output.OutputValue
		case "ClusterSecurityGroup":
			result.clusterVpcConfig.securityGroup = *output.OutputValue
		case "PodIdentityAssociationRoleARN":
			result.podIdentity.roleArn = *output.OutputValue
		case "PodIdentityS3BucketName":
			result.podIdentity.s3Bucket = *output.OutputValue
		}
	}
	return result
}

func (s *stack) tagKeyPairSSMParameter(ctx context.Context, clusterName string) error {
	keyPair, err := peered.KeyPair(ctx, s.ec2Client, clusterName)
	if err != nil {
		return err
	}
	_, err = s.ssmClient.AddTagsToResource(ctx, &ssm.AddTagsToResourceInput{
		ResourceId:   aws.String("/ec2/keypair/" + *keyPair.KeyPairId),
		ResourceType: ssmTypes.ResourceTypeForTaggingParameter,
		Tags: []ssmTypes.Tag{
			{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(clusterName),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("tagging private key ssm parameter: %w", err)
	}
	return nil
}

func (s *stack) setupJumpbox(ctx context.Context, clusterName string) error {
	jumpbox, err := peered.JumpboxInstance(ctx, s.ec2Client, clusterName)
	if err != nil {
		return err
	}

	instanceProfileName := strings.Split(*jumpbox.IamInstanceProfile.Arn, "/")[2]
	_, err = s.iamClient.TagInstanceProfile(ctx, &iam.TagInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
		Tags: []iamTypes.Tag{
			{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(clusterName),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("tagging jumpbox instance profile: %w", err)
	}

	if err := e2eSSM.WaitForInstance(ctx, s.ssmClient, *jumpbox.InstanceId, s.logger); err != nil {
		return fmt.Errorf("waiting for jumpbox instance to be registered with ssm: %w", err)
	}

	command := "/root/download-private-key.sh"
	var output e2eCommands.RemoteCommandOutput

	// Retry private key download to handle IAM role credential propagation timing.
	// This is different from the retry for SSM call inside RunCommand since it is not the SSM API call that fails,
	// but the command execution on the jumpbox done by that SSM API call that fails due to IAM role credentials
	// not being available through IMDS yet, even though the instance profile is attached.
	for range 3 {
		output, err = e2eSSM.RunCommand(ctx, s.ssmClient, *jumpbox.InstanceId, command, s.logger)
		if err != nil {
			return fmt.Errorf("jumpbox getting private key from ssm: %w", err)
		}
		if output.Status == "Success" {
			break
		}
		s.logger.Info("Private key download failed, retrying in 10 seconds")
		time.Sleep(10 * time.Second)
	}

	if output.Status != "Success" {
		return fmt.Errorf("jumpbox getting private key from ssm after retries")
	}

	return nil
}

func stackName(clusterName string) string {
	return fmt.Sprintf("%s-%s", constants.TestArchitectureStackNamePrefix, clusterName)
}

func (s *stack) delete(ctx context.Context, clusterName string) error {
	stackName := stackName(clusterName)
	s.logger.Info("Deleting E2E test cluster stack", "stackName", stackName)

	if err := s.emptyPodIdentityS3Bucket(ctx, clusterName); err != nil {
		return fmt.Errorf("deleting pod identity s3 bucket: %w", err)
	}

	cfnCleaner := cleanup.NewCFNStackCleanup(s.cfn, s.logger)
	if err := cfnCleaner.DeleteStack(ctx, stackName); err != nil {
		return fmt.Errorf("deleting hybrid nodes setup cfn stack: %w", err)
	}

	s.logger.Info("E2E test cluster stack deleted successfully", "stackName", stackName)
	return nil
}

func (s *stack) emptyPodIdentityS3Bucket(ctx context.Context, clusterName string) error {
	podIdentityBucket, err := addon.PodIdentityBucket(ctx, s.s3Client, clusterName)
	if err != nil {
		if errors.Is(err, addon.ErrPodIdentityBucketNotFound) {
			return nil
		}
		return fmt.Errorf("getting pod identity s3 bucket: %w", err)
	}

	s3cleaner := cleanup.NewS3Cleaner(s.s3Client, s.logger)
	s.logger.Info("Empty pod identity s3 bucket", "bucket", podIdentityBucket)
	if err = s3cleaner.EmptyS3Bucket(ctx, podIdentityBucket); err != nil {
		return fmt.Errorf("emptying pod identity s3 bucket: %w", err)
	}

	return nil
}

func replaceCreationTimeParameter(existingParams, newParams []cfnTypes.Parameter) []cfnTypes.Parameter {
	// if the stack already exists, try to find the original creation time
	// and replace the new parameter with the original one to avoid triggering an
	// unnecessary update
	var existingCreationTime string
	for _, param := range existingParams {
		if *param.ParameterKey == creationTimeParameterKey {
			existingCreationTime = *param.ParameterValue
			break
		}
	}
	if existingCreationTime == "" {
		return newParams
	}

	for i, newParam := range newParams {
		if *newParam.ParameterKey == creationTimeParameterKey {
			newParams[i].ParameterValue = aws.String(existingCreationTime)
			break
		}
	}
	return newParams
}

func ensureAllStackOutputsNonEmpty(stackOut *resourcesStackOutput) error {
	var missing []string

	if stackOut.clusterRole == "" {
		missing = append(missing, "ClusterRole")
	}
	if stackOut.clusterVpcConfig.vpcID == "" {
		missing = append(missing, "ClusterVPC")
	}
	if stackOut.clusterVpcConfig.publicSubnet == "" {
		missing = append(missing, "ClusterVPCPublicSubnet")
	}
	if stackOut.clusterVpcConfig.privateSubnet == "" {
		missing = append(missing, "ClusterVPCPrivateSubnet")
	}
	if stackOut.clusterVpcConfig.securityGroup == "" {
		missing = append(missing, "ClusterSecurityGroup")
	}
	if stackOut.podIdentity.roleArn == "" {
		missing = append(missing, "PodIdentityAssociationRoleARN")
	}
	if stackOut.podIdentity.s3Bucket == "" {
		missing = append(missing, "PodIdentityS3BucketName")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required CloudFormation outputs after stack deployment: %v", missing)
	}

	return nil
}
