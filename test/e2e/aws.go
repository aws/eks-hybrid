package e2e

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go/aws/awserr"
)

func NewAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	// Create a new config using shared credentials or environment variables
	config, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to create new AWS config: %v", err)
	}

	return config, nil
}

func isErrCode(err error, code string) bool {
	if err == nil {
		return false
	}
	if awsErr, ok := err.(awserr.Error); ok {
		return awsErr.Code() == code
	}

	return false
}

// SanitizeForAWSName removes everything except alphanumeric characters and hyphens from a string.
func SanitizeForAWSName(input string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9-]+`)
	return re.ReplaceAllString(input, "")
}

// GetTruncatedName truncates the string name based on limit AWS puts on names
func GetTruncatedName(name string, limit int) string {
	if len(name) > limit {
		name = name[:limit]
	}
	return name
}

func IsErrorType(err error, targetType interface{}) bool {
	return errors.As(err, &targetType)
}
