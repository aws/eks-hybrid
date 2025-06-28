package ecr

import (
	"testing"

	awsinternal "github.com/aws/eks-hybrid/internal/aws"
)

func TestGetEKSRegistryCoordinates(t *testing.T) {
	tests := []struct {
		name         string
		region       string
		regionConfig *awsinternal.RegionData
		expectAcct   string
		expectRegion string
	}{
		{
			name:   "manifest data available - takes precedence",
			region: "us-east-1",
			regionConfig: &awsinternal.RegionData{
				EcrAccountID: "123456789012",
			},
			expectAcct:   "123456789012",
			expectRegion: "us-east-1",
		},
		{
			name:   "manifest data available for non-commercial region - takes precedence over hardcoded",
			region: "cn-north-1",
			regionConfig: &awsinternal.RegionData{
				EcrAccountID: "123456789012",
			},
			expectAcct:   "123456789012",
			expectRegion: "cn-north-1",
		},
		{
			name:         "non-commercial region - cn-north-1 from hardcoded map",
			region:       "cn-north-1",
			regionConfig: nil,
			expectAcct:   "918309763551",
			expectRegion: "cn-north-1",
		},
		{
			name:         "non-commercial region - cn-northwest-1 from hardcoded map",
			region:       "cn-northwest-1",
			regionConfig: nil,
			expectAcct:   "961992271922",
			expectRegion: "cn-northwest-1",
		},
		{
			name:         "non-commercial region - us-gov-east-1 from hardcoded map",
			region:       "us-gov-east-1",
			regionConfig: nil,
			expectAcct:   "151742754352",
			expectRegion: "us-gov-east-1",
		},
		{
			name:         "non-commercial region - us-gov-west-1 from hardcoded map",
			region:       "us-gov-west-1",
			regionConfig: nil,
			expectAcct:   "013241004608",
			expectRegion: "us-gov-west-1",
		},
		{
			name:         "non-commercial region - us-iso-east-1 from hardcoded map",
			region:       "us-iso-east-1",
			regionConfig: nil,
			expectAcct:   "725322719131",
			expectRegion: "us-iso-east-1",
		},
		{
			name:         "non-commercial region - us-iso-west-1 from hardcoded map",
			region:       "us-iso-west-1",
			regionConfig: nil,
			expectAcct:   "608367168043",
			expectRegion: "us-iso-west-1",
		},
		{
			name:         "non-commercial region - us-isob-east-1 from hardcoded map",
			region:       "us-isob-east-1",
			regionConfig: nil,
			expectAcct:   "187977181151",
			expectRegion: "us-isob-east-1",
		},
		{
			name:         "non-commercial region - us-isof-south-1 from hardcoded map",
			region:       "us-isof-south-1",
			regionConfig: nil,
			expectAcct:   "676585237158",
			expectRegion: "us-isof-south-1",
		},
		{
			name:         "non-commercial region - eu-isoe-west-1 from hardcoded map",
			region:       "eu-isoe-west-1",
			regionConfig: nil,
			expectAcct:   "249663109785",
			expectRegion: "eu-isoe-west-1",
		},
		{
			name:         "prefix fallback - unknown us-gov region",
			region:       "us-gov-unknown-1",
			regionConfig: nil,
			expectAcct:   "013241004608",
			expectRegion: "us-gov-west-1",
		},
		{
			name:         "prefix fallback - unknown cn region",
			region:       "cn-unknown-1",
			regionConfig: nil,
			expectAcct:   "961992271922",
			expectRegion: "cn-northwest-1",
		},
		{
			name:         "prefix fallback - unknown us-iso region",
			region:       "us-iso-unknown-1",
			regionConfig: nil,
			expectAcct:   "725322719131",
			expectRegion: "us-iso-east-1",
		},
		{
			name:         "prefix fallback - unknown us-isob region",
			region:       "us-isob-unknown-1",
			regionConfig: nil,
			expectAcct:   "187977181151",
			expectRegion: "us-isob-east-1",
		},
		{
			name:         "prefix fallback - unknown us-isof region",
			region:       "us-isof-unknown-1",
			regionConfig: nil,
			expectAcct:   "676585237158",
			expectRegion: "us-isof-south-1",
		},
		{
			name:         "default fallback - commercial region not in manifest",
			region:       "us-east-1",
			regionConfig: nil,
			expectAcct:   "602401143452",
			expectRegion: "us-west-2",
		},
		{
			name:         "default fallback - unknown region",
			region:       "unknown-region",
			regionConfig: nil,
			expectAcct:   "602401143452",
			expectRegion: "us-west-2",
		},
		{
			name:   "empty ECR account ID in manifest - falls back to hardcoded for non-commercial",
			region: "cn-north-1",
			regionConfig: &awsinternal.RegionData{
				EcrAccountID: "",
			},
			expectAcct:   "918309763551",
			expectRegion: "cn-north-1",
		},
		{
			name:   "empty ECR account ID in manifest - falls back to default for commercial",
			region: "us-east-1",
			regionConfig: &awsinternal.RegionData{
				EcrAccountID: "",
			},
			expectAcct:   "602401143452",
			expectRegion: "us-west-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account, region := getEKSRegistryCoordinatesWithRegionConfig(tt.region, tt.regionConfig)
			if account != tt.expectAcct {
				t.Errorf("Expected account %s, got %s", tt.expectAcct, account)
			}
			if region != tt.expectRegion {
				t.Errorf("Expected region %s, got %s", tt.expectRegion, region)
			}
		})
	}
}
