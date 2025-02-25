package cleanup

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

type ResourceTagger struct {
	ResourceTagging *resourcegroupstaggingapi.Client
	ClusterName     string
}

// GetTaggedResources returns all resources of the specified type that are tagged with this cluster's name
func (r *ResourceTagger) GetTaggedResources(ctx context.Context, resourceType string) ([]string, error) {
	input := &resourcegroupstaggingapi.GetResourcesInput{
		ResourceTypeFilters: []string{resourceType},
		TagFilters: []types.TagFilter{
			{
				Key: aws.String(constants.TestClusterTagKey),
			},
		},
	}
	// if clusterName is empty then we are either deleting by prefix or all clusters
	// if not empty add the filter to limit the results to the specified cluster
	if r.ClusterName != "" {
		input.TagFilters[0].Values = []string{r.ClusterName}
	}

	paginator := resourcegroupstaggingapi.NewGetResourcesPaginator(r.ResourceTagging, input)

	var resources []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing tagged resources of type %s: %w", resourceType, err)
		}

		for _, resource := range output.ResourceTagMappingList {
			resources = append(resources, *resource.ResourceARN)
		}
	}

	return resources, nil
}
