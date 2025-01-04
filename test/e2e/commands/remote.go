package commands

import "context"

type RemoteCommandRunner interface {
	Run(ctx context.Context, ip string, commands []string) (RemoteCommandOutput, error)
}

type RemoteCommandOutput struct {
	ResponseCode   int32
	StandardError  string
	StandardOutput string
	Status         string
}
