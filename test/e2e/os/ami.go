package os

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// AMI represents an ec2 Image.
type AMI struct {
	ID        string
	CreatedAt time.Time
}

// findLatestImage returns the most recent AMI matching the amiPrefix and arch and owned by ownerAccount
func findLatestImage(ctx context.Context, client *ec2.Client, ownerAccount, amiPrefix, arch string) (string, error) {
	var latestAMI AMI

	in := &ec2.DescribeImagesInput{
		Owners:     []string{ownerAccount},
		Filters:    []types.Filter{{Name: aws.String("name"), Values: []string{amiPrefix}}, {Name: aws.String("architecture"), Values: []string{arch}}},
		MaxResults: aws.Int32(100),
	}

	for {
		l, err := client.DescribeImages(ctx, in)
		if err != nil {
			return "", err
		}

		if paginationDone(in, l) {
			break
		}

		for _, i := range l.Images {
			created, err := time.Parse(time.RFC3339Nano, *i.CreationDate)
			if err != nil {
				return "", err
			}
			if created.Compare(latestAMI.CreatedAt) > 0 {
				latestAMI = AMI{
					ID:        *i.ImageId,
					CreatedAt: created,
				}
			}
		}

		in.NextToken = l.NextToken

		if in.NextToken == nil {
			break
		}
	}

	return latestAMI.ID, nil
}

func paginationDone(in *ec2.DescribeImagesInput, out *ec2.DescribeImagesOutput) bool {
	// Filters work on the returned output per page. Its important we go through all the pages
	// and only end pagination when next token == input token
	return out.NextToken != nil && in.NextToken == out.NextToken
}
