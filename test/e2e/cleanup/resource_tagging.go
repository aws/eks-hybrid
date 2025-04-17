package cleanup

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	resourcegroupstaggingTypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

type ResourceTaggingClient struct {
	client *resourcegroupstaggingapi.Client
}

func NewResourceTaggingClient(client *resourcegroupstaggingapi.Client) *ResourceTaggingClient {
	return &ResourceTaggingClient{
		client: client,
	}
}

// GetResourcesWithClusterTag returns a array of resourceARNs for the resourceType with the given cluster tag
// this is useful for getting resources which do not allow filtering by tag in their native api
// and (usually) an additional api call per resource to get the tags
// which we need to determine if we want to delete the resource
func (c *ResourceTaggingClient) GetResourcesWithClusterTag(ctx context.Context, resourceType string, filterInput FilterInput) (map[string][]Tag, error) {
	input := &resourcegroupstaggingapi.GetResourcesInput{
		ResourceTypeFilters: []string{resourceType},
	}
	if filterInput.ClusterName != "" {
		input.TagFilters = []resourcegroupstaggingTypes.TagFilter{
			{
				Key:    aws.String(constants.TestClusterTagKey),
				Values: []string{filterInput.ClusterName},
			},
		}
	} else {
		input.TagFilters = []resourcegroupstaggingTypes.TagFilter{
			{
				Key: aws.String(constants.TestClusterTagKey),
			},
		}
	}

	resourceARNs := map[string][]Tag{}
	paginator := resourcegroupstaggingapi.NewGetResourcesPaginator(c.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing resources: %w", err)
		}

		for _, resource := range page.ResourceTagMappingList {
			var customTags []Tag
			for _, tag := range resource.Tags {
				customTags = append(customTags, Tag{
					Key:   *tag.Key,
					Value: *tag.Value,
				})
			}
			resourceARNs[aws.ToString(resource.ResourceARN)] = customTags
		}
	}

	return resourceARNs, nil
}
