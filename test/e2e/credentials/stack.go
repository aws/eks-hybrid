package credentials

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfnTypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamTypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/cfn"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	e2errors "github.com/aws/eks-hybrid/test/e2e/errors"
)

const (
	stackCreationTimeout = 5 * time.Minute
	stackDeletionTimeout = 8 * time.Minute
	stackRetryDelay      = 5 * time.Second
)

//go:embed cfn-templates/hybrid-cfn.yaml
var cfnTemplateBody string

type Stack struct {
	ClusterName            string
	Name                   string
	ClusterArn             string
	CFN                    *cloudformation.Client
	IAM                    *iam.Client
	EKS                    *eks.Client
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
	ManagedNodeRoleArn string `json:"managedNodeRoleArn"`
}

func (s *Stack) Deploy(ctx context.Context, logger logr.Logger) (*StackOutput, error) {
	// There are occasional race conditions when creating the cfn stack
	// retrying once allows to potentially resolve them on the second attempt
	// avoiding the need to retry the entire test suite.
	var err error
	for range 2 {
		if err = s.deployStack(ctx, logger); err == nil {
			break
		}
		logger.Error(err, "Error deploying stack, retrying")
	}
	if err != nil {
		return nil, fmt.Errorf("deploying credentials stack: %w", err)
	}

	output, err := s.readStackOutput(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("reading stack output: %w", err)
	}

	logger.Info("Creating access entry", "ssmRoleArn", output.SSMNodeRoleARN)
	_, err = s.EKS.CreateAccessEntry(ctx, &eks.CreateAccessEntryInput{
		ClusterName:  &s.ClusterName,
		PrincipalArn: &output.SSMNodeRoleARN,
		Type:         aws.String("HYBRID_LINUX"),
	}, func(o *eks.Options) {
		o.Retryer = retry.AddWithErrorCodes(o.Retryer, "InvalidParameterException")
	})
	if err != nil && !isResourceAlreadyInUse(err) {
		return nil, err
	}

	if !skipIRATest() {
		logger.Info("Creating access entry", "iamRoleArn", output.IRANodeRoleARN)
		_, err = s.EKS.CreateAccessEntry(ctx, &eks.CreateAccessEntryInput{
			ClusterName:  &s.ClusterName,
			PrincipalArn: &output.IRANodeRoleARN,
			Type:         aws.String("HYBRID_LINUX"),
		}, func(o *eks.Options) {
			o.Retryer = retry.AddWithErrorCodes(o.Retryer, "InvalidParameterException")
		})
		if err != nil && !isResourceAlreadyInUse(err) {
			return nil, err
		}
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
	resp, err := s.CFN.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(s.Name),
	})
	if err != nil && !e2errors.IsCFNStackNotFound(err) {
		return fmt.Errorf("looking for hybrid nodes cfn stack: %w", err)
	}
	params := []cfnTypes.Parameter{
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
		{
			ParameterKey:   aws.String("rolePathPrefix"),
			ParameterValue: aws.String(constants.TestRolePathPrefix),
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
	if resp == nil || len(resp.Stacks) == 0 {
		logger.Info("Creating hybrid nodes stack", "stackName", s.Name)
		_, err = s.CFN.CreateStack(ctx, &cloudformation.CreateStackInput{
			DisableRollback: aws.Bool(true),
			StackName:       aws.String(s.Name),
			TemplateBody:    aws.String(buf.String()),
			Parameters:      params,
			Capabilities: []cfnTypes.Capability{
				cfnTypes.CapabilityCapabilityNamedIam,
			},
			Tags: []cfnTypes.Tag{{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(s.ClusterName),
			}},
		})
		if err != nil {
			return fmt.Errorf("creating hybrid nodes cfn stack: %w", err)
		}
		if err := cfn.WaitForStackOperation(ctx, s.CFN, s.Name, stackRetryDelay, stackCreationTimeout); err != nil {
			return err
		}
	} else if resp.Stacks[0].StackStatus == cfnTypes.StackStatusCreateInProgress {
		logger.Info("Waiting for hybrid nodes stack to be created", "stackName", s.Name)
		if err := cfn.WaitForStackOperation(ctx, s.CFN, s.Name, stackRetryDelay, stackCreationTimeout); err != nil {
			return err
		}
	} else {
		logger.Info("Updating hybrid nodes stack", "stackName", s.Name)
		_, err = s.CFN.UpdateStack(ctx, &cloudformation.UpdateStackInput{
			DisableRollback: aws.Bool(true),
			StackName:       aws.String(s.Name),
			Capabilities: []cfnTypes.Capability{
				cfnTypes.CapabilityCapabilityNamedIam,
			},
			TemplateBody: aws.String(buf.String()),
			Parameters:   params,
		})
		var apiErr smithy.APIError
		if ok := errors.As(err, &apiErr); err != nil && (!ok || apiErr.ErrorMessage() != "No updates are to be performed.") {
			return fmt.Errorf("updating hybrid nodes cfn stack: %w", err)
		} else if ok && apiErr.ErrorMessage() == "No updates are to be performed." {
			logger.Info("No updates are to be performed for hybrid nodes stack", "stackName", s.Name)
			// Skip waiting for update completion since no update occurred
			return nil
		}

		logger.Info("Waiting for hybrid nodes stack to be updated", "stackName", s.Name)
		if err := cfn.WaitForStackOperation(ctx, s.CFN, s.Name, stackRetryDelay, stackCreationTimeout); err != nil {
			return err
		}
	}

	return nil
}

func (s *Stack) createInstanceProfile(ctx context.Context, logger logr.Logger, roleName string) (instanceProfileArn string, err error) {
	instanceProfileName := s.instanceProfileName(roleName)

	instanceProfile, err := s.IAM.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
	})
	var instanceProfileHasRole bool
	if isNotFound(err) {
		logger.Info("Creating instance profile", "instanceProfileName", instanceProfileName)
		instanceProfileArnOut, err := s.IAM.CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{
			InstanceProfileName: aws.String(instanceProfileName),
			Path:                aws.String(constants.TestRolePathPrefix),
			Tags: []iamTypes.Tag{{
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
		_, err = s.IAM.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{
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
	// Retry logic to wait for stack outputs to be populated
	ticker := time.NewTicker(stackRetryDelay)
	defer ticker.Stop()
	timeout := time.After(stackCreationTimeout)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for stack outputs to be populated after %v", stackCreationTimeout)
		case <-ticker.C:
			resp, err := s.CFN.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
				StackName: aws.String(s.Name),
			})
			if err != nil {
				return nil, fmt.Errorf("describing hybrid nodes cfn stack: %w", err)
			}

			if len(resp.Stacks) == 0 {
				logger.Info("Stack not found, retrying...", "stackName", s.Name)
				continue
			}

			stack := resp.Stacks[0]
			if len(stack.Outputs) == 0 {
				logger.Info("Stack outputs are empty, waiting for outputs to be populated...", "stackName", s.Name)
				continue
			}

			result := &StackOutput{}
			// extract relevant stack outputs
			for _, output := range stack.Outputs {
				if output.OutputKey == nil || output.OutputValue == nil {
					continue
				}
				switch *output.OutputKey {
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
				case "ManagedNodeRoleArn":
					result.ManagedNodeRoleArn = *output.OutputValue
				}
			}

			// Check if all expected outputs are populated and non-empty
			var missingOutputs []string

			// Always required outputs
			if result.EC2Role == "" {
				missingOutputs = append(missingOutputs, "EC2Role")
			}
			if result.SSMNodeRoleName == "" {
				missingOutputs = append(missingOutputs, "SSMNodeRoleName")
			}
			if result.SSMNodeRoleARN == "" {
				missingOutputs = append(missingOutputs, "SSMNodeRoleARN")
			}
			if result.ManagedNodeRoleArn == "" {
				missingOutputs = append(missingOutputs, "ManagedNodeRoleArn")
			}

			// IRA-related outputs (when IRA test is not skipped)
			if !skipIRATest() {
				if result.IRANodeRoleName == "" {
					missingOutputs = append(missingOutputs, "IRANodeRoleName")
				}
				if result.IRANodeRoleARN == "" {
					missingOutputs = append(missingOutputs, "IRANodeRoleARN")
				}
				if result.IRATrustAnchorARN == "" {
					missingOutputs = append(missingOutputs, "IRATrustAnchorARN")
				}
				if result.IRAProfileARN == "" {
					missingOutputs = append(missingOutputs, "IRAProfileARN")
				}
			}

			// If any outputs are missing, continue waiting
			if len(missingOutputs) > 0 {
				logger.Info("Stack outputs are still empty, waiting for outputs to be populated...",
					"stackName", s.Name, "missingOutputs", missingOutputs)
				continue
			}

			// All required outputs are populated and non-empty
			logger.Info("E2E resources stack deployed successfully", "stackName", s.Name)
			return result, nil
		}
	}
}

func (s *Stack) Delete(ctx context.Context, logger logr.Logger, output *StackOutput) error {
	instanceProfileName := s.instanceProfileName(output.EC2Role)
	logger.Info("Deleting instance profile", "instanceProfileName", instanceProfileName)
	instanceProfile, err := s.IAM.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
	})
	if err != nil {
		return err
	}
	if _, err := s.IAM.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
		RoleName:            instanceProfile.InstanceProfile.Roles[0].RoleName,
	}); err != nil {
		return err
	}
	if _, err := s.IAM.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
	}); err != nil {
		return fmt.Errorf("deleting instance profile: %w", err)
	}

	logger.Info("Deleting access entry", "ssmRoleArn", output.SSMNodeRoleARN)
	if _, err := s.EKS.DeleteAccessEntry(ctx, &eks.DeleteAccessEntryInput{
		ClusterName:  &s.ClusterName,
		PrincipalArn: &output.SSMNodeRoleARN,
	}); err != nil {
		return fmt.Errorf("deleting SSM access entry: %w", err)
	}
	if !skipIRATest() {
		logger.Info("Deleting access entry", "iamRoleArn", output.IRANodeRoleARN)
		if _, err := s.EKS.DeleteAccessEntry(ctx, &eks.DeleteAccessEntryInput{
			ClusterName:  &s.ClusterName,
			PrincipalArn: &output.IRANodeRoleARN,
		}); err != nil {
			return fmt.Errorf("deleting iam-ra access entry: %w", err)
		}
	}

	cfnCleaner := cleanup.NewCFNStackCleanup(s.CFN, logger)
	err = cfnCleaner.DeleteStack(ctx, s.Name)
	if err != nil {
		return fmt.Errorf("deleting hybrid nodes cfn stack: %w", err)
	}

	logger.Info("E2E resources stack deleted successfully", "stackName", s.Name)
	return nil
}

func isNotFound(err error) bool {
	return e2errors.IsAwsError(err, "NoSuchEntity")
}

func skipIRATest() bool {
	return os.Getenv("SKIP_IRA_TEST") == "true"
}

func isResourceAlreadyInUse(err error) bool {
	return e2errors.IsAwsError(err, "ResourceInUseException")
}
