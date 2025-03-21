package run

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
)

type E2EArtifacts struct {
	ArtifactsFolder string
	AwsCfg          aws.Config
	Logger          logr.Logger
	LogsBucket      string
	LogsBucketPath  string
}

func (e *E2EArtifacts) Upload(ctx context.Context) error {
	if e.LogsBucket == "" {
		return nil
	}

	s3Client := s3.NewFromConfig(e.AwsCfg)
	err := filepath.WalkDir(e.ArtifactsFolder, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking artifacts folder: %w", err)
		}
		if info.IsDir() {
			return nil
		}

		fileRelPath := strings.Replace(path, e.ArtifactsFolder+"/", "", 1)
		keyPath := fmt.Sprintf("%s/%s", e.LogsBucketPath, fileRelPath)

		if err := e.uploadLogsToS3(ctx, s3Client, path, keyPath); err != nil {
			return fmt.Errorf("uploading test log to s3: %w", err)
		}

		return nil
	})

	return err
}

func (e *E2EArtifacts) uploadLogsToS3(ctx context.Context, s3Client *s3.Client, localFile, s3Key string) error {
	logContent, err := os.ReadFile(localFile)
	if err != nil {
		return fmt.Errorf("failed to read test log file: %w", err)
	}
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(e.LogsBucket),
		Key:    aws.String(s3Key),
		Body:   bytes.NewReader(logContent),
	})
	if err != nil {
		return fmt.Errorf("failed to upload test log to s3: %w", err)
	}
	return nil
}
