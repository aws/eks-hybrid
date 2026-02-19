package aws

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePartitionFromARN(t *testing.T) {
	tests := []struct {
		name      string
		arn       string
		want      string
		wantError bool
	}{
		{
			name: "standard AWS partition",
			arn:  "arn:aws:iam::123456789012:user/test-user",
			want: "aws",
		},
		{
			name: "AWS China partition",
			arn:  "arn:aws-cn:iam::123456789012:user/test-user",
			want: "aws-cn",
		},
		{
			name: "AWS GovCloud partition",
			arn:  "arn:aws-us-gov:iam::123456789012:user/test-user",
			want: "aws-us-gov",
		},
		{
			name: "AWS ISO partition",
			arn:  "arn:aws-iso:iam::123456789012:user/test-user",
			want: "aws-iso",
		},
		{
			name: "AWS ISO-B partition",
			arn:  "arn:aws-iso-b:iam::123456789012:user/test-user",
			want: "aws-iso-b",
		},
		{
			name: "AWS ISO-E partition",
			arn:  "arn:aws-iso-e:iam::123456789012:user/test-user",
			want: "aws-iso-e",
		},
		{
			name: "AWS ISO-F partition",
			arn:  "arn:aws-iso-f:iam::123456789012:user/test-user",
			want: "aws-iso-f",
		},
		{
			name:      "invalid ARN - no colon",
			arn:       "invalid-arn",
			wantError: true,
		},
		{
			name:      "invalid ARN - empty",
			arn:       "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePartitionFromARN(tt.arn)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetPartitionDNSSuffix(t *testing.T) {
	tests := []struct {
		name      string
		partition string
		want      string
	}{
		{
			name:      "AWS standard partition",
			partition: "aws",
			want:      "amazonaws.com",
		},
		{
			name:      "AWS China partition",
			partition: "aws-cn",
			want:      "amazonaws.com.cn",
		},
		{
			name:      "AWS GovCloud partition",
			partition: "aws-us-gov",
			want:      "amazonaws.com",
		},
		{
			name:      "AWS ISO partition",
			partition: "aws-iso",
			want:      "c2s.ic.gov",
		},
		{
			name:      "AWS ISO-B partition",
			partition: "aws-iso-b",
			want:      "sc2s.sgov.gov",
		},
		{
			name:      "AWS ISO-E partition",
			partition: "aws-iso-e",
			want:      "cloud.adc-e.uk",
		},
		{
			name:      "AWS ISO-F partition",
			partition: "aws-iso-f",
			want:      "csp.hci.ic.gov",
		},
		{
			name:      "unknown partition defaults to standard",
			partition: "unknown",
			want:      "amazonaws.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPartitionDNSSuffix(tt.partition)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetServiceEndpointForPartition(t *testing.T) {
	tests := []struct {
		name      string
		service   string
		region    string
		partition string
		want      string
	}{
		{
			name:      "EKS in standard AWS",
			service:   "eks",
			region:    "us-west-2",
			partition: "aws",
			want:      "eks.us-west-2.amazonaws.com",
		},
		{
			name:      "EKS in AWS China",
			service:   "eks",
			region:    "cn-north-1",
			partition: "aws-cn",
			want:      "eks.cn-north-1.amazonaws.com.cn",
		},
		{
			name:      "EKS in AWS GovCloud",
			service:   "eks",
			region:    "us-gov-west-1",
			partition: "aws-us-gov",
			want:      "eks.us-gov-west-1.amazonaws.com",
		},
		{
			name:      "STS in AWS ISO",
			service:   "sts",
			region:    "us-iso-east-1",
			partition: "aws-iso",
			want:      "sts.us-iso-east-1.c2s.ic.gov",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetServiceEndpointForPartition(tt.service, tt.region, tt.partition)
			assert.Equal(t, tt.want, got)
		})
	}
}
