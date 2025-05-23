package cleanup

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/errors"
)

type RolesAnywhereCleaner struct {
	rolesAnywhere *rolesanywhere.Client
	taggingClient *ResourceTaggingClient
	logger        logr.Logger
}

func NewRolesAnywhereCleaner(rolesAnywhere *rolesanywhere.Client, taggingClient *ResourceTaggingClient, logger logr.Logger) *RolesAnywhereCleaner {
	return &RolesAnywhereCleaner{
		rolesAnywhere: rolesAnywhere,
		taggingClient: taggingClient,
		logger:        logger,
	}
}

func shouldDeleteProfile(profile types.ProfileDetail, tags []Tag, filterInput FilterInput) bool {
	resource := ResourceWithTags{
		ID:           *profile.Name,
		CreationTime: aws.ToTime(profile.CreatedAt),
		Tags:         tags,
	}

	return shouldDeleteResource(resource, filterInput)
}

// ex: arn:aws:rolesanywhere:us-west-2:<account>:profile/<profile-id>
// ex: arn:aws:rolesanywhere:us-west-2:<account>:trust-anchor/<anchor-id>
func parseResourceIdFromARN(arn string) string {
	parts := strings.Split(arn, "/")
	return parts[len(parts)-1]
}

func (c *RolesAnywhereCleaner) ListProfiles(ctx context.Context, filterInput FilterInput) ([]string, error) {
	var profiles []string

	// we use resourcetaggingapi here since rolesanywhere list api does not support filtering
	// nor does it return the tags, whiuch means we end up having to make a number of api requests
	// just to determine that we do not wish to delete this resource
	// resourcetaggingapi only gives the profiles we care about, based on the filterInput
	resourceARNs, err := c.taggingClient.GetResourcesWithClusterTag(ctx, "rolesanywhere:profile", filterInput)
	if err != nil {
		return nil, fmt.Errorf("listing roles anywhere profiles: %w", err)
	}
	for resourceARN, tags := range resourceARNs {
		profileID := parseResourceIdFromARN(resourceARN)

		profileOutput, err := c.rolesAnywhere.GetProfile(ctx, &rolesanywhere.GetProfileInput{
			ProfileId: aws.String(profileID),
		})
		if err != nil && errors.IsType(err, &types.ResourceNotFoundException{}) {
			// skipping log since we are possiblying checking profiles we do not
			// intend to delete
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("getting profile details: %w", err)
		}

		profile := profileOutput.Profile
		if shouldDeleteProfile(*profile, tags, filterInput) {
			profiles = append(profiles, *profile.ProfileId)
		}

	}

	return profiles, nil
}

func (c *RolesAnywhereCleaner) DeleteProfile(ctx context.Context, profileID string) error {
	_, err := c.rolesAnywhere.DeleteProfile(ctx, &rolesanywhere.DeleteProfileInput{
		ProfileId: aws.String(profileID),
	})
	if err != nil && errors.IsType(err, &types.ResourceNotFoundException{}) {
		c.logger.Info("Roles Anywhere profile already deleted", "profileID", profileID)

		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting roles anywhere profile %s: %w", profileID, err)
	}
	c.logger.Info("Deleted Roles Anywhere profile", "profileID", profileID)
	return nil
}

func shouldDeleteTrustAnchor(anchor types.TrustAnchorDetail, tags []Tag, filterInput FilterInput) bool {
	resource := ResourceWithTags{
		ID:           *anchor.Name,
		CreationTime: aws.ToTime(anchor.CreatedAt),
		Tags:         tags,
	}

	return shouldDeleteResource(resource, filterInput)
}

func (c *RolesAnywhereCleaner) ListTrustAnchors(ctx context.Context, filterInput FilterInput) ([]string, error) {
	var anchors []string

	// we use resourcetaggingapi here since rolesanywhere list trust-anchor api does not support filtering
	// nor does it return the tags, whiuch means we end up having to make a number of api requests
	// just to determine that we do not wish to delete this resource
	// resourcetaggingapi only gives the trust anchors we care about, based on the filterInput
	resourceARNs, err := c.taggingClient.GetResourcesWithClusterTag(ctx, "rolesanywhere:trust-anchor", filterInput)
	if err != nil {
		return nil, fmt.Errorf("listing roles anywhere trust anchors: %w", err)
	}
	for resourceARN, tags := range resourceARNs {
		anchorID := parseResourceIdFromARN(resourceARN)
		anchorOutput, err := c.rolesAnywhere.GetTrustAnchor(ctx, &rolesanywhere.GetTrustAnchorInput{
			TrustAnchorId: aws.String(anchorID),
		})
		if err != nil && errors.IsType(err, &types.ResourceNotFoundException{}) {
			// skipping log since we are possiblying checking trust anchors we do not
			// intend to delete
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("getting trust anchor details: %w", err)
		}
		if shouldDeleteTrustAnchor(*anchorOutput.TrustAnchor, tags, filterInput) {
			anchors = append(anchors, anchorID)
		}
	}

	return anchors, nil
}

func (c *RolesAnywhereCleaner) DeleteTrustAnchor(ctx context.Context, anchorID string) error {
	_, err := c.rolesAnywhere.DeleteTrustAnchor(ctx, &rolesanywhere.DeleteTrustAnchorInput{
		TrustAnchorId: aws.String(anchorID),
	})
	if err != nil && errors.IsType(err, &types.ResourceNotFoundException{}) {
		c.logger.Info("Roles Anywhere trust anchor already deleted", "anchorID", anchorID)

		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting roles anywhere trust anchor %s: %w", anchorID, err)
	}
	c.logger.Info("Deleted Roles Anywhere trust anchor", "anchorID", anchorID)
	return nil
}
