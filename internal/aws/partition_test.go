package aws

import (
	"testing"
)

func TestParsePartitionFromARN(t *testing.T) {
	tests := []struct {
		name      string
		arn       string
		want      string
		wantError bool
	}{
		{
			name: "StandardAWS",
			arn:  "arn:aws:iam::123456789012:role/MyRole",
			want: "aws",
		},
		{
			name: "AWSChina",
			arn:  "arn:aws-cn:iam::123456789012:role/MyRole",
			want: "aws-cn",
		},
		{
			name: "GovCloud",
			arn:  "arn:aws-us-gov:iam::123456789012:role/MyRole",
			want: "aws-us-gov",
		},
		{
			name:      "InvalidNoPrefix",
			arn:       "not-an-arn",
			wantError: true,
		},
		{
			name:      "InvalidTooShort",
			arn:       "arn:",
			wantError: true,
		},
		{
			name:      "Empty",
			arn:       "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePartitionFromARN(tt.arn)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPartitionDNSSuffix(t *testing.T) {
	tests := []struct {
		partition string
		want      string
	}{
		{"aws", "amazonaws.com"},
		{"aws-cn", "amazonaws.com.cn"},
		{"aws-us-gov", "amazonaws.com"},
		{"aws-iso", "c2s.ic.gov"},
		{"aws-iso-b", "sc2s.sgov.gov"},
		{"aws-iso-e", "cloud.adc-e.uk"},
		{"aws-iso-f", "csp.hci.ic.gov"},
		{"aws-eusc", "amazonaws.eu"},
		{"unknown", "amazonaws.com"}, // default case
	}

	for _, tt := range tests {
		t.Run(tt.partition, func(t *testing.T) {
			got := GetPartitionDNSSuffix(tt.partition)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
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
			name:      "StandardAWS",
			service:   "eks",
			region:    "us-east-1",
			partition: "aws",
			want:      "eks.us-east-1.amazonaws.com",
		},
		{
			name:      "China",
			service:   "eks",
			region:    "cn-north-1",
			partition: "aws-cn",
			want:      "eks.cn-north-1.amazonaws.com.cn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetServiceEndpointForPartition(tt.service, tt.region, tt.partition)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEC2ServicePrincipal(t *testing.T) {
	if GetEC2ServicePrincipal("aws-cn") != "ec2.amazonaws.com.cn" {
		t.Errorf("aws-cn should return ec2.amazonaws.com.cn")
	}
	if GetEC2ServicePrincipal("aws") != "ec2.amazonaws.com" {
		t.Errorf("aws should return ec2.amazonaws.com")
	}
}

func TestGetPartitionFromRegionFallback(t *testing.T) {
	tests := []struct {
		region string
		want   string
	}{
		{"us-east-1", "aws"},
		{"eu-west-1", "aws"},
		{"cn-north-1", "aws-cn"},
		{"cn-test", "aws-cn"},
		{"us-gov-west-1", "aws-us-gov"},
		{"us-iso-east-1", "aws-iso"},
		{"us-isob-east-1", "aws-iso-b"},
		{"us-isoe-east-1", "aws-iso-e"},
		{"us-isof-south-1", "aws-iso-f"},
		{"eusc-de-east-1", "aws-eusc"},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			got := GetPartitionFromRegionFallback(tt.region)
			if got != tt.want {
				t.Errorf("GetPartitionFromRegionFallback(%q) = %v, want %v", tt.region, got, tt.want)
			}
		})
	}
}
