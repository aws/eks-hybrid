package images_test

import (
	"testing"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/images"
)

func TestNginx(t *testing.T) {
	tests := []struct {
		name       string
		region     string
		dnsSuffix  string
		ecrAccount string
		want       string
	}{
		{
			name:       "china region uses public ecr directly",
			region:     "cn-northwest-1",
			dnsSuffix:  "amazonaws.com.cn",
			ecrAccount: constants.ChinaEcrAccountId,
			want:       "public.ecr.aws/nginx/nginx:latest",
		},
		{
			name:       "other china region uses public ecr directly",
			region:     "cn-north-1",
			dnsSuffix:  "amazonaws.com.cn",
			ecrAccount: constants.ChinaEcrAccountId,
			want:       "public.ecr.aws/nginx/nginx:latest",
		},
		{
			name:       "commercial region uses pull through cache",
			region:     "us-west-2",
			dnsSuffix:  "amazonaws.com",
			ecrAccount: constants.EcrAccountId,
			want:       "381492195191.dkr.ecr.us-west-2.amazonaws.com/ecr-public/nginx/nginx:latest",
		},
		{
			name:       "govcloud region uses pull through cache",
			region:     "us-gov-west-1",
			dnsSuffix:  "amazonaws.com",
			ecrAccount: constants.EcrAccountId,
			want:       "381492195191.dkr.ecr.us-gov-west-1.amazonaws.com/ecr-public/nginx/nginx:latest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := images.Nginx(tc.region, tc.dnsSuffix, tc.ecrAccount); got != tc.want {
				t.Errorf("Nginx(%q, %q, %q) = %q, want %q", tc.region, tc.dnsSuffix, tc.ecrAccount, got, tc.want)
			}
		})
	}
}

func TestAwsCli(t *testing.T) {
	tests := []struct {
		name       string
		region     string
		dnsSuffix  string
		ecrAccount string
		want       string
	}{
		{
			name:       "china region uses public ecr directly",
			region:     "cn-northwest-1",
			dnsSuffix:  "amazonaws.com.cn",
			ecrAccount: constants.ChinaEcrAccountId,
			want:       "public.ecr.aws/aws-cli/aws-cli:latest",
		},
		{
			name:       "commercial region uses pull through cache",
			region:     "us-west-2",
			dnsSuffix:  "amazonaws.com",
			ecrAccount: constants.EcrAccountId,
			want:       "381492195191.dkr.ecr.us-west-2.amazonaws.com/ecr-public/aws-cli/aws-cli:latest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := images.AwsCli(tc.region, tc.dnsSuffix, tc.ecrAccount); got != tc.want {
				t.Errorf("AwsCli(%q, %q, %q) = %q, want %q", tc.region, tc.dnsSuffix, tc.ecrAccount, got, tc.want)
			}
		})
	}
}

func TestIsChinaRegion(t *testing.T) {
	tests := []struct {
		region string
		want   bool
	}{
		{region: "cn-north-1", want: true},
		{region: "cn-northwest-1", want: true},
		{region: "us-west-2", want: false},
		{region: "us-gov-west-1", want: false},
		{region: "eu-central-1", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.region, func(t *testing.T) {
			if got := images.IsChinaRegion(tc.region); got != tc.want {
				t.Errorf("IsChinaRegion(%q) = %v, want %v", tc.region, got, tc.want)
			}
		})
	}
}
