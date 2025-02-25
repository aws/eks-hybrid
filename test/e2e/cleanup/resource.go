package cleanup

import (
	"strings"
	"time"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

type Tag struct {
	Key   string
	Value string
}

type ResourceWithTags struct {
	ID           string
	CreationTime time.Time
	Tags         []Tag
}

func getClusterTagValue(tags []Tag) string {
	var clusterTagValue string
	for _, tag := range tags {
		if tag.Key == constants.TestClusterTagKey {
			clusterTagValue = tag.Value
			break
		}
	}
	return clusterTagValue
}

func shouldDeleteResource(resource ResourceWithTags, input FilterInput) bool {
	clusterTagValue := getClusterTagValue(resource.Tags)
	if clusterTagValue == "" {
		return false
	}

	// For exact cluster name match, delete regardless of age
	if input.ClusterName != "" {
		return clusterTagValue == input.ClusterName
	}

	// For all clusters or prefix match, check resource age
	if input.AllClusters || (input.ClusterNamePrefix != "" && strings.HasPrefix(clusterTagValue, input.ClusterNamePrefix)) {
		resourceAge := time.Since(resource.CreationTime)
		return resourceAge > input.InstanceAgeThreshold
	}

	return false
}
