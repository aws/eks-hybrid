package os

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/test/e2e"
)

func TestAmazonLinux2023BuildUserDataPrivateMode(t *testing.T) {
	input := e2e.UserDataInput{
		KubernetesVersion: "1.35",
		NodeadmUrls:       e2e.NodeadmURLs{AMD: "https://bucket.s3.us-west-2.amazonaws.com/nodeadm/amd64/nodeadm"},
		NodeadmConfig:     &api.NodeConfig{},
		Provider:          "ssm",
		Region:            "us-west-2",
		ManifestURL:       "https://bucket.s3.us-west-2.amazonaws.com/manifests/manifest.yaml",
		PrivateMode:       true,
		HostsEntries: []e2e.HostsEntry{
			{IP: "10.0.0.10", Hostname: "ssm.us-west-2.amazonaws.com"},
			{IP: "10.1.0.20", Hostname: "ABC.gr7.us-west-2.eks.amazonaws.com"},
		},
	}

	userData, err := NewAmazonLinux2023AMD().BuildUserData(context.Background(), input)
	if err != nil {
		t.Fatalf("BuildUserData() error = %v", err)
	}
	got := string(userData)

	for _, want := range []string{
		`echo "10.0.0.10 ssm.us-west-2.amazonaws.com" >> /etc/hosts`,
		`echo "10.1.0.20 ABC.gr7.us-west-2.eks.amazonaws.com" >> /etc/hosts`,
		"dnf install -y containerd iptables",
		`NODEADM_ARGS=("${NODEADM_ARGS[@]}" "--private-mode")`,
		`"--manifest-override" "https://bucket.s3.us-west-2.amazonaws.com/manifests/manifest.yaml"`,
		`/tmp/nodeadm-init.sh "https://bucket.s3.us-west-2.amazonaws.com/nodeadm/amd64/nodeadm"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("user data missing %q", want)
		}
	}

	// Hosts entries and packages must be in place before nodeadm runs.
	if strings.Index(got, "/etc/hosts") > strings.Index(got, "/tmp/nodeadm-init.sh \"") ||
		strings.Index(got, "dnf install -y containerd") > strings.Index(got, "/tmp/nodeadm-init.sh \"") {
		t.Errorf("hosts entries and package install should run before nodeadm-init.sh")
	}
}

func TestAmazonLinux2023BuildUserDataDefaultMode(t *testing.T) {
	input := e2e.UserDataInput{
		KubernetesVersion: "1.35",
		NodeadmUrls:       e2e.NodeadmURLs{AMD: "https://example.com/nodeadm"},
		NodeadmConfig:     &api.NodeConfig{},
		Provider:          "ssm",
		Region:            "us-west-2",
	}

	userData, err := NewAmazonLinux2023AMD().BuildUserData(context.Background(), input)
	if err != nil {
		t.Fatalf("BuildUserData() error = %v", err)
	}
	got := string(userData)

	for _, unwanted := range []string{"--private-mode", "/etc/hosts", "dnf install -y containerd"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("user data should not contain %q without private mode", unwanted)
		}
	}
}
