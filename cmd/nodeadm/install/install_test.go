package install

import (
	"testing"

	"github.com/integrii/flaggy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerdVersionFlag(t *testing.T) {
	for _, version := range []string{"", "1.7.27-1.amzn2023.0.1", "1:1.7.27-0ubuntu1"} {
		t.Run(version, func(t *testing.T) {
			cmd := NewCommand().(*command)
			parser := flaggy.NewParser("nodeadm")
			parser.AttachSubcommand(cmd.Flaggy(), 1)
			args := []string{"install", "1.31", "--credential-provider", "ssm"}
			if version != "" {
				args = append(args, "--containerd-version", version)
			}
			require.NoError(t, parser.ParseArgs(args))
			assert.True(t, cmd.Flaggy().Used)
			assert.Equal(t, version, cmd.containerdVersion)
			assert.Equal(t, "1.31", cmd.kubernetesVersion)
		})
	}
}
