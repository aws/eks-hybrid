package cleanup

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

type IAMCleaner struct {
	IAM    *iam.Client
	Logger logr.Logger
}

func (c *IAMCleaner) ListRoles(ctx context.Context, filterInput FilterInput) ([]string, error) {
	var roles []string

	// list-roles does not allow filtering by tags so we have to pull them all
	// We have the role =* checks to try and limit which roles we bother checking tags for
	// but we only delete those with the e2e cluster tag
	paginator := iam.NewListRolesPaginator(c.IAM, &iam.ListRolesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing IAM roles: %w", err)
		}
		for _, role := range page.Roles {
			if !strings.HasPrefix(*role.RoleName, constants.TestCredentialsStackNamePrefix) {
				continue
			}
			paginator := iam.NewListRoleTagsPaginator(c.IAM, &iam.ListRoleTagsInput{
				RoleName: role.RoleName,
			})
			var tags []types.Tag
			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					return nil, fmt.Errorf("listing IAM role tags: %w", err)
				}
				tags = append(tags, page.Tags...)
			}
			role.Tags = tags
			if shouldDeleteRole(role, filterInput) {
				roles = append(roles, *role.RoleName)
			}
		}
	}
	return roles, nil
}

func shouldDeleteRole(role types.Role, input FilterInput) bool {
	var tags []Tag
	for _, tag := range role.Tags {
		tags = append(tags, Tag{
			Key:   *tag.Key,
			Value: *tag.Value,
		})
	}
	resource := ResourceWithTags{
		ID:           *role.RoleName,
		CreationTime: aws.ToTime(role.CreateDate),
		Tags:         tags,
	}
	return shouldDeleteResource(resource, input)
}

func (c *IAMCleaner) DeleteRole(ctx context.Context, roleName string) error {
	instanceProfiles, err := c.IAM.ListInstanceProfilesForRole(ctx, &iam.ListInstanceProfilesForRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil && errors.IsType(err, &types.NoSuchEntityException{}) {
		c.Logger.Info("IAM instance profiles already deleted", "roleName", roleName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("listing IAM instance profiles: %w", err)
	}
	for _, instanceProfile := range instanceProfiles.InstanceProfiles {
		_, err := c.IAM.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
			InstanceProfileName: instanceProfile.InstanceProfileName,
			RoleName:            aws.String(roleName),
		})
		if err != nil {
			return fmt.Errorf("removing role from instance profile %s: %w", *instanceProfile.InstanceProfileName, err)
		}
	}

	_, err = c.IAM.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil && errors.IsType(err, &types.NoSuchEntityException{}) {
		c.Logger.Info("IAM role already deleted", "roleName", roleName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting IAM role %s: %w", roleName, err)
	}
	c.Logger.Info("Deleted IAM role", "roleName", roleName)
	return nil
}

func (c *IAMCleaner) ListInstanceProfiles(ctx context.Context, filterInput FilterInput) ([]string, error) {
	var instanceProfiles []string

	// list-instance-profiles does not allow filtering by tags so we have to pull them all
	// We have the role =* checks to try and limit which roles we bother checking tags for
	// but we only delete those with the e2e cluster tag
	paginator := iam.NewListInstanceProfilesPaginator(c.IAM, &iam.ListInstanceProfilesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing IAM instance profiles: %w", err)
		}
		for _, profile := range page.InstanceProfiles {
			if !strings.HasPrefix(*profile.InstanceProfileName, constants.TestCredentialsStackNamePrefix) {
				continue
			}
			paginator := iam.NewListInstanceProfileTagsPaginator(c.IAM, &iam.ListInstanceProfileTagsInput{
				InstanceProfileName: profile.InstanceProfileName,
			})
			var tags []types.Tag
			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					return nil, fmt.Errorf("listing IAM instance profile tags: %w", err)
				}
				tags = append(tags, page.Tags...)
			}
			profile.Tags = tags
			if shouldDeleteInstanceProfile(profile, filterInput) {
				instanceProfiles = append(instanceProfiles, *profile.InstanceProfileName)
			}
		}
	}
	return instanceProfiles, nil
}

func shouldDeleteInstanceProfile(profile types.InstanceProfile, input FilterInput) bool {
	var tags []Tag
	for _, tag := range profile.Tags {
		tags = append(tags, Tag{
			Key:   *tag.Key,
			Value: *tag.Value,
		})
	}
	resource := ResourceWithTags{
		ID:           *profile.InstanceProfileName,
		CreationTime: aws.ToTime(profile.CreateDate),
		Tags:         tags,
	}
	return shouldDeleteResource(resource, input)
}

func (c *IAMCleaner) DeleteInstanceProfile(ctx context.Context, profileName string) error {
	_, err := c.IAM.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
		InstanceProfileName: aws.String(profileName),
	})
	if err != nil && errors.IsType(err, &types.NoSuchEntityException{}) {
		c.Logger.Info("IAM instance profile already deleted", "profileName", profileName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting IAM instance profile %s: %w", profileName, err)
	}
	c.Logger.Info("Deleted IAM instance profile", "profileName", profileName)
	return nil
}
