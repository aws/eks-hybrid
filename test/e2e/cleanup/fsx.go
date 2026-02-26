package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const (
	fsxDeletionTimeout      = 15 * time.Minute
	fsxDeletionPollInterval = 30 * time.Second
)

type FSxCleaner struct {
	fsxClient *fsx.Client
	ec2Client *ec2.Client
	logger    logr.Logger
}

func NewFSxCleaner(fsxClient *fsx.Client, ec2Client *ec2.Client, logger logr.Logger) *FSxCleaner {
	return &FSxCleaner{
		fsxClient: fsxClient,
		ec2Client: ec2Client,
		logger:    logger,
	}
}

func (f *FSxCleaner) ListTaggedFileSystems(ctx context.Context, input FilterInput) ([]string, error) {
	testVPCIDs, err := f.listTestVPCIDs(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("listing test VPC IDs: %w", err)
	}

	paginator := fsx.NewDescribeFileSystemsPaginator(f.fsxClient, &fsx.DescribeFileSystemsInput{})

	var fileSystemIDs []string

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing FSx file systems: %w", err)
		}

		for _, fs := range page.FileSystems {
			if shouldDeleteFileSystem(fs, input) {
				fileSystemIDs = append(fileSystemIDs, *fs.FileSystemId)
			} else if isOrphanedTestFileSystem(fs, input, testVPCIDs) {
				fileSystemIDs = append(fileSystemIDs, *fs.FileSystemId)
			}
		}
	}

	return fileSystemIDs, nil
}

func (f *FSxCleaner) listTestVPCIDs(ctx context.Context, input FilterInput) (map[string]bool, error) {
	vpcCleaner := NewVPCCleaner(f.ec2Client, f.logger)
	vpcIDs, err := vpcCleaner.ListVPCs(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("listing VPCs: %w", err)
	}

	vpcSet := make(map[string]bool, len(vpcIDs))
	for _, id := range vpcIDs {
		vpcSet[id] = true
	}
	return vpcSet, nil
}

func (f *FSxCleaner) DeleteFileSystems(ctx context.Context, fileSystemIDs []string) error {
	for _, fsID := range fileSystemIDs {
		f.logger.Info("Deleting FSx file system", "fileSystemId", fsID)
		_, err := f.fsxClient.DeleteFileSystem(ctx, &fsx.DeleteFileSystemInput{
			FileSystemId: aws.String(fsID),
		})
		if errors.IsType(err, &fsxtypes.FileSystemNotFound{}) {
			f.logger.Info("FSx file system already deleted", "fileSystemId", fsID)
			continue
		}
		if err != nil {
			return fmt.Errorf("deleting FSx file system %s: %w", fsID, err)
		}
	}

	for _, fsID := range fileSystemIDs {
		if err := f.waitForDeletion(ctx, fsID); err != nil {
			return fmt.Errorf("waiting for FSx file system %s deletion: %w", fsID, err)
		}
	}

	return nil
}

func (f *FSxCleaner) waitForDeletion(ctx context.Context, fileSystemID string) error {
	return wait.PollUntilContextTimeout(ctx, fsxDeletionPollInterval, fsxDeletionTimeout, true, func(ctx context.Context) (bool, error) {
		output, err := f.fsxClient.DescribeFileSystems(ctx, &fsx.DescribeFileSystemsInput{
			FileSystemIds: []string{fileSystemID},
		})
		if errors.IsType(err, &fsxtypes.FileSystemNotFound{}) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("describing FSx file system %s: %w", fileSystemID, err)
		}
		if len(output.FileSystems) == 0 {
			return true, nil
		}

		f.logger.Info("Waiting for FSx file system deletion", "fileSystemId", fileSystemID)
		return false, nil
	})
}

func shouldDeleteFileSystem(fs fsxtypes.FileSystem, input FilterInput) bool {
	var tags []Tag
	for _, tag := range fs.Tags {
		tags = append(tags, Tag{
			Key:   aws.ToString(tag.Key),
			Value: aws.ToString(tag.Value),
		})
	}

	resource := ResourceWithTags{
		ID:           aws.ToString(fs.FileSystemId),
		CreationTime: aws.ToTime(fs.CreationTime),
		Tags:         tags,
	}
	return shouldDeleteResource(resource, input)
}

// isOrphanedTestFileSystem identifies FSx Lustre filesystems that were created
// by tests before tagging was added. It checks if the filesystem is in a tagged
// test VPC and is older than the age threshold.
func isOrphanedTestFileSystem(fs fsxtypes.FileSystem, input FilterInput, testVPCIDs map[string]bool) bool {
	if fs.FileSystemType == fsxtypes.FileSystemTypeLustre &&
		testVPCIDs[aws.ToString(fs.VpcId)] &&
		input.InstanceAgeThreshold > 0 {
		if time.Since(aws.ToTime(fs.CreationTime)) > input.InstanceAgeThreshold {
			return true
		}
	}

	return false
}
