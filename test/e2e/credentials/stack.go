package credentials

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/go-logr/logr"
)

//go:embed cfn-templates/hybrid-cfn.yaml
var cfnTemplateBody string

type Stack struct {
	ClusterName            string
	Name                   string
	ClusterArn             string
	CFN                    *cloudformation.CloudFormation
	IAM                    *iam.IAM
	IAMRolesAnywhereCACert []byte
}

type hybridCfnTemplateVars struct {
	IncludeRolesAnywhere bool
}

type StackOutput struct {
	EC2Role            string `json:"EC2Role"`
	InstanceProfileARN string `json:"instanceProfileARN"`
	SSMNodeRoleName    string `json:"ssmNodeRoleName"`
	SSMNodeRoleARN     string `json:"ssmNodeRoleARN"`
	IRANodeRoleName    string `json:"iraNodeRoleName"`
	IRANodeRoleARN     string `json:"iraNodeRoleARN"`
	IRATrustAnchorARN  string `json:"iraTrustAnchorARN"`
	IRAProfileARN      string `json:"iraProfileARN"`
}

func (s *Stack) Deploy(ctx context.Context, logger logr.Logger) (*StackOutput, error) {
	if err := s.deployStack(ctx, logger); err != nil {
		return nil, err
	}

	output, err := s.readStackOutput(ctx, logger)
	if err != nil {
		return nil, err
	}

	// We create the instance profile manually instead of as part of the CFN stack because it's faster.
	// This sucks because of the complexity it adds both to create and delete, having to deal with
	// partial creations where the instance profile might exist already and role might have been added or not.
	// But it speeds up the test about 2.5 minutes, so it's worth it. For some reason, creating
	// instance profiles from CFN
	// is very slow: https://repost.aws/questions/QUoU5UybeUR2S2iYNEJiStiQ/cloudformation-provisioning-of-aws-iam-instanceprofile-takes-a-long-time
	// I suspect this is because CFN has a hardcoded ~2.5 minutes wait after instance profile creation before
	// considering it "created". Probably to avoid eventual consistency issues when using the instance profile in
	// another resource immediately after. We have to deal with that problem ourselves now by retrying the ec2 instance
	// creation on "invalid IAM instance profile" error.
	output.InstanceProfileARN, err = s.createInstanceProfile(ctx, logger, output.EC2Role)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (s *Stack) deployStack(ctx context.Context, logger logr.Logger) error {
	resp, err := s.CFN.DescribeStacksWithContext(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(s.Name),
	})
	if aerr, ok := err.(awserr.Error); ok && aerr.Code() != "ValidationError" {
		return fmt.Errorf("looking for hybrid nodes cfn stack: %w", err)
	}
	params := []*cloudformation.Parameter{
		{
			ParameterKey:   aws.String("clusterName"),
			ParameterValue: aws.String(s.ClusterName),
		},
		{
			ParameterKey:   aws.String("clusterArn"),
			ParameterValue: aws.String(s.ClusterArn),
		},
		{
			ParameterKey:   aws.String("caBundleCert"),
			ParameterValue: aws.String(string(s.IAMRolesAnywhereCACert)),
		},
	}

	var buf bytes.Buffer
	cfnTemplate, err := template.New("hybridCfnTemplate").Parse(cfnTemplateBody)
	if err != nil {
		return fmt.Errorf("parsing hybrid-cfn.yaml template: %w", err)
	}
	cfnTemplateConfig := &hybridCfnTemplateVars{IncludeRolesAnywhere: !skipIRATest()}
	err = cfnTemplate.Execute(&buf, cfnTemplateConfig)
	if err != nil {
		return fmt.Errorf("applying data to hybrid-cfn.yaml template: %w", err)
	}
	if len(resp.Stacks) == 0 {
		logger.Info("Creating hybrid nodes stack", "stackName", s.Name)
		_, err = s.CFN.CreateStackWithContext(ctx, &cloudformation.CreateStackInput{
			StackName:    aws.String(s.Name),
			TemplateBody: aws.String(buf.String()),
			Parameters:   params,
			Capabilities: []*string{
				aws.String("CAPABILITY_NAMED_IAM"),
			},
			Tags: []*cloudformation.Tag{{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(s.ClusterName),
			}},
		})
		if err != nil {
			return fmt.Errorf("creating hybrid nodes cfn stack: %w", err)
		}

		logger.Info("Waiting for hybrid nodes stack to be created", "stackName", s.Name)
		err = s.CFN.WaitUntilStackCreateCompleteWithContext(ctx, &cloudformation.DescribeStacksInput{
			StackName: aws.String(s.Name),
		}, request.WithWaiterDelay(request.ConstantWaiterDelay(2*time.Second)))
		if err != nil {
			return fmt.Errorf("waiting for hybrid nodes cfn stack: %w", err)
		}
	} else {
		logger.Info("Updating hybrid nodes stack", "stackName", s.Name)
		_, err = s.CFN.UpdateStackWithContext(ctx, &cloudformation.UpdateStackInput{
			StackName: aws.String(s.Name),
			Capabilities: []*string{
				aws.String("CAPABILITY_NAMED_IAM"),
			},
			TemplateBody: aws.String(buf.String()),
			Parameters:   params,
		})

		if aerr, ok := err.(awserr.Error); err != nil && (!ok || aerr.Message() != "No updates are to be performed.") {
			return fmt.Errorf("updating hybrid nodes cfn stack: %w", err)
		} else if ok && aerr.Message() == "No updates are to be performed." {
			logger.Info("No updates are to be performed for hybrid nodes stack", "stackName", s.Name)
			// Skip waiting for update completion since no update occurred
			return nil
		}

		logger.Info("Waiting for hybrid nodes stack to be updated", "stackName", s.Name)
		err = s.CFN.WaitUntilStackUpdateCompleteWithContext(ctx, &cloudformation.DescribeStacksInput{
			StackName: aws.String(s.Name),
		}, request.WithWaiterDelay(request.ConstantWaiterDelay(5*time.Second)))
		if err != nil {
			return fmt.Errorf("waiting for hybrid nodes cfn stack: %w", err)
		}
	}

	return nil
}

func (s *Stack) createInstanceProfile(ctx context.Context, logger logr.Logger, roleName string) (instanceProfileArn string, err error) {
	instanceProfileName := s.instanceProfileName(roleName)

	instanceProfile, err := s.IAM.GetInstanceProfileWithContext(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
	})
	var instanceProfileHasRole bool
	if isNotFound(err) {
		logger.Info("Creating instance profile", "instanceProfileName", instanceProfileName)
		instanceProfileArnOut, err := s.IAM.CreateInstanceProfileWithContext(ctx, &iam.CreateInstanceProfileInput{
			InstanceProfileName: aws.String(instanceProfileName),
			Path:                aws.String("/"),
			Tags: []*iam.Tag{{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(s.ClusterName),
			}},
		})
		if err != nil {
			return "", err
		}
		instanceProfileArn = *instanceProfileArnOut.InstanceProfile.Arn
		instanceProfileHasRole = false
	} else if err != nil {
		return "", err
	} else {
		logger.Info("Instance profile already exists", "instanceProfileName", instanceProfileName)
		instanceProfileArn = *instanceProfile.InstanceProfile.Arn
		if len(instanceProfile.InstanceProfile.Roles) > 0 {
			instanceProfileHasRole = true
		} else {
			instanceProfileHasRole = false
		}
	}

	if instanceProfileHasRole {
		logger.Info("Instance profile already has a role", "instanceProfileName", instanceProfileName)
	} else {
		logger.Info("Adding role to instance profile", "roleName", roleName)
		_, err = s.IAM.AddRoleToInstanceProfileWithContext(ctx, &iam.AddRoleToInstanceProfileInput{
			InstanceProfileName: aws.String(instanceProfileName),
			RoleName:            aws.String(roleName),
		})
		if err != nil {
			return "", err
		}

	}

	return instanceProfileArn, nil
}

func (s *Stack) instanceProfileName(roleName string) string {
	return roleName
}

func (s *Stack) readStackOutput(ctx context.Context, logger logr.Logger) (*StackOutput, error) {
	resp, err := s.CFN.DescribeStacksWithContext(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(s.Name),
	})
	if err != nil {
		return nil, fmt.Errorf("describing hybrid nodes cfn stack: %w", err)
	}

	result := &StackOutput{}
	// extract relevant stack outputs
	for _, output := range resp.Stacks[0].Outputs {
		switch aws.StringValue(output.OutputKey) {
		case "EC2Role":
			result.EC2Role = *output.OutputValue
		case "SSMNodeRoleName":
			result.SSMNodeRoleName = *output.OutputValue
		case "SSMNodeRoleARN":
			result.SSMNodeRoleARN = *output.OutputValue
		case "IRANodeRoleName":
			result.IRANodeRoleName = *output.OutputValue
		case "IRANodeRoleARN":
			result.IRANodeRoleARN = *output.OutputValue
		case "IRATrustAnchorARN":
			result.IRATrustAnchorARN = *output.OutputValue
		case "IRAProfileARN":
			result.IRAProfileARN = *output.OutputValue
		}
	}

	logger.Info("E2E resources stack deployed successfully", "stackName", s.Name)
	return result, nil
}

func (s *Stack) Delete(ctx context.Context, logger logr.Logger, output *StackOutput) error {
	instanceProfileName := s.instanceProfileName(output.EC2Role)
	logger.Info("Deleting instance profile", "instanceProfileName", instanceProfileName)
	instanceProfile, err := s.IAM.GetInstanceProfileWithContext(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
	})
	if err != nil {
		return err
	}
	if _, err := s.IAM.RemoveRoleFromInstanceProfileWithContext(ctx, &iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
		RoleName:            instanceProfile.InstanceProfile.Roles[0].RoleName,
	}); err != nil {
		return err
	}
	if _, err := s.IAM.DeleteInstanceProfileWithContext(ctx, &iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
	}); err != nil {
		return fmt.Errorf("deleting instance profile: %w", err)
	}

	_, err = s.CFN.DeleteStackWithContext(ctx, &cloudformation.DeleteStackInput{
		StackName: aws.String(s.Name),
	})
	if err != nil {
		return fmt.Errorf("deleting hybrid nodes cfn stack: %w", err)
	}
	err = s.CFN.WaitUntilStackDeleteCompleteWithContext(ctx,
		&cloudformation.DescribeStacksInput{StackName: aws.String(s.Name)},
		request.WithWaiterDelay(request.ConstantWaiterDelay(2*time.Second)),
		request.WithWaiterMaxAttempts(240))
	if err != nil {
		return fmt.Errorf("waiting for hybrid nodes cfn stack: %w", err)
	}
	logger.Info("E2E resources stack deleted successfully", "stackName", s.Name)
	return nil
}

func isNotFound(err error) bool {
	aerr, ok := err.(awserr.Error)
	return err != nil && ok && aerr.Code() == "NoSuchEntity"
}

func skipIRATest() bool {
	return os.Getenv("SKIP_IRA_TEST") == "true"
}
