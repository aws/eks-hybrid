package ssm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/ssm"
)

func TestWaitForAWSConfigSuccess(t *testing.T) {
	g := NewWithT(t)
	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Region: "us-west-2",
			},
		},
	}

	credsDir := t.TempDir()
	credsFile := filepath.Join(credsDir, "credentials")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsFile)

	go func() {
		time.Sleep(2 * time.Millisecond)
		g.Expect(
			os.WriteFile(credsFile, []byte("[default]\naws_access_key_id=foo\naws_secret_access_key=bar\n"), 0o644),
		).To(Succeed())
	}()

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	config, err := ssm.WaitForAWSConfig(ctx, node, 1*time.Millisecond)
	g.Expect(err).To(Succeed())
	g.Expect(config).NotTo(BeZero())
	g.Expect(config.Region).To(Equal("us-west-2"))
}

func TestWaitForAWSConfigTimeout(t *testing.T) {
	g := NewWithT(t)
	node := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Region: "us-west-2",
			},
		},
	}

	credsDir := t.TempDir()
	credsFile := filepath.Join(credsDir, "credentials")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsFile)

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	config, err := ssm.WaitForAWSConfig(ctx, node, 1*time.Millisecond)
	g.Expect(err).To(MatchError(ContainSubstring("ssm AWS creds file " + credsFile + " hasn't been created on time")))
	g.Expect(config).To(BeZero())
}
