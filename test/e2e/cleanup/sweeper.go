package cleanup

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/go-logr/logr"
)

type SweeperInput struct {
	AllClusters          bool          `yaml:"allClusters"`
	DryRun               bool          `yaml:"dryRun"`
	ClusterName          string        `yaml:"clusterName"`
	ClusterNamePrefix    string        `yaml:"clusterNamePrefix"`
	InstanceAgeThreshold time.Duration `yaml:"instanceAgeThreshold"`
}

type Sweeper struct {
	cfn           *cloudformation.Client
	ec2Client     *ec2.Client
	eks           *eks.Client
	iam           *iam.Client
	logger        logr.Logger
	s3Client      *s3.Client
	ssm           *ssm.Client
	rolesAnywhere *rolesanywhere.Client
}

type FilterInput struct {
	ClusterName          string
	ClusterNamePrefix    string
	AllClusters          bool
	InstanceAgeThreshold time.Duration
	DryRun               bool
}

func NewSweeper(aws aws.Config, logger logr.Logger) Sweeper {
	return Sweeper{
		cfn:           cloudformation.NewFromConfig(aws),
		ec2Client:     ec2.NewFromConfig(aws),
		eks:           eks.NewFromConfig(aws),
		iam:           iam.NewFromConfig(aws),
		logger:        logger,
		ssm:           ssm.NewFromConfig(aws),
		s3Client:      s3.NewFromConfig(aws),
		rolesAnywhere: rolesanywhere.NewFromConfig(aws),
	}
}

func (c *Sweeper) Run(ctx context.Context, input SweeperInput) error {
	filterInput := FilterInput{
		ClusterName:          input.ClusterName,
		ClusterNamePrefix:    input.ClusterNamePrefix,
		AllClusters:          input.AllClusters,
		InstanceAgeThreshold: input.InstanceAgeThreshold,
		DryRun:               input.DryRun,
	}

	if filterInput.DryRun {
		c.logger.Info("Dry run enabled, skipping deletions")
	}

	// Deletion ordering
	// - instances
	// - instance profiles since these are created after the test cfn
	//   - remove roles from instance profile first
	// - test credential cfn statck (creates the ssm/iam-ra roles/ec2role)
	// - eks clusters
	// - infra cfn stack (creates vpcs/eks cluster)
	// - remaining leaking items which should only exist in the case where the cfn deletion is incomplete

	return nil
}
