package cleanup

import (
	"context"
	"errors"
	"fmt"
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

	"github.com/aws/eks-hybrid/test/e2e"
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

func NewSweeper(aws aws.Config, logger logr.Logger, endpoint string) Sweeper {
	return Sweeper{
		cfn:           cloudformation.NewFromConfig(aws),
		ec2Client:     ec2.NewFromConfig(aws),
		eks:           e2e.NewEKSClient(aws, endpoint),
		iam:           iam.NewFromConfig(aws),
		logger:        logger,
		ssm:           ssm.NewFromConfig(aws),
		s3Client:      s3.NewFromConfig(aws),
		rolesAnywhere: rolesanywhere.NewFromConfig(aws),
	}
}

type Cleanup struct {
	Cleanup        func(ctx context.Context, filterInput FilterInput) error
	FailureMessage string
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
	// - empty s3 pod identity buckets, infra cfn stack deletes the bucket
	// - infra cfn stack (creates vpcs/eks cluster)
	// - remaining leaking items which should only exist in the case where the cfn deletion is incomplete
	cleanups := []Cleanup{
		{
			Cleanup:        c.cleanupEC2Instances,
			FailureMessage: "cleaning up EC2 instances",
		},
		{
			Cleanup:        c.cleanupIAMInstanceProfiles,
			FailureMessage: "cleaning up IAM instance profiles",
		},
		{
			Cleanup:        c.cleanupCredentialStacks,
			FailureMessage: "cleaning up credential stacks",
		},
		{
			Cleanup:        c.cleanupEKSClusters,
			FailureMessage: "cleaning up EKS clusters",
		},
		{
			Cleanup:        c.emptyS3PodIdentityBuckets,
			FailureMessage: "emptying S3 pod identity buckets",
		},
		{
			Cleanup:        c.cleanupArchitectureStacks,
			FailureMessage: "cleaning up architecture stacks",
		},
		{
			Cleanup:        c.cleanupRolesAnywhereProfiles,
			FailureMessage: "cleaning up Roles Anywhere profiles",
		},
		{
			Cleanup:        c.cleanupRolesAnywhereTrustAnchors,
			FailureMessage: "cleaning up Roles Anywhere trust anchors",
		},
		{
			Cleanup:        c.cleanupIAMRoles,
			FailureMessage: "cleaning up IAM roles",
		},
		{
			Cleanup:        c.cleanupPeeringConnections,
			FailureMessage: "cleaning up peering connections",
		},
		{
			Cleanup:        c.cleanupInternetGateways,
			FailureMessage: "cleaning up internet gateways",
		},
		{
			Cleanup:        c.cleanupNetworkInterfaces,
			FailureMessage: "cleaning up network interfaces",
		},
		{
			Cleanup:        c.cleanupTransitGateways,
			FailureMessage: "cleaning up transit gateways",
		},
		{
			Cleanup:        c.cleanupSubnets,
			FailureMessage: "cleaning up subnets",
		},
		{
			Cleanup:        c.cleanupRouteTables,
			FailureMessage: "cleaning up route tables",
		},
		{
			Cleanup:        c.cleanupSecurityGroups,
			FailureMessage: "cleaning up security groups",
		},
		{
			Cleanup:        c.cleanupVPCs,
			FailureMessage: "cleaning up VPCs",
		},
		{
			Cleanup:        c.cleanupS3PodIdentityBuckets,
			FailureMessage: "cleaning up S3 pod identity buckets",
		},
		{
			Cleanup:        c.cleanupKeyPairs,
			FailureMessage: "cleaning up key pairs",
		},
		{
			Cleanup:        c.cleanupSSMParameters,
			FailureMessage: "cleaning up SSM parameters",
		},
		{
			Cleanup:        c.cleanupSSMManagedInstances,
			FailureMessage: "cleaning up SSM managed instances",
		},
		{
			Cleanup:        c.cleanupSSMHybridActivations,
			FailureMessage: "cleaning up SSM hybrid activations",
		},
	}

	allErrors := []error{}
	for _, cleanup := range cleanups {
		if err := cleanup.Cleanup(ctx, filterInput); err != nil {
			c.logger.Error(err, cleanup.FailureMessage)
			allErrors = append(allErrors, fmt.Errorf("%s: %w", cleanup.FailureMessage, err))
		}
	}

	if len(allErrors) > 0 {
		return errors.Join(allErrors...)
	}

	return nil
}

func (c *Sweeper) cleanupSSMManagedInstances(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewSSMCleaner(c.ssm, c.logger)
	instanceIds, err := cleaner.ListManagedInstances(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing managed instances: %w", err)
	}

	c.logger.Info("Deleting managed instances", "instanceIds", instanceIds)
	if filterInput.DryRun {
		return nil
	}

	for _, instanceID := range instanceIds {
		if err := cleaner.DeleteManagedInstance(ctx, instanceID); err != nil {
			return fmt.Errorf("deleting managed instance: %w", err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupSSMHybridActivations(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewSSMCleaner(c.ssm, c.logger)
	activationIDs, err := cleaner.ListActivations(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing activations: %w", err)
	}

	c.logger.Info("Deleting activations", "activationIDs", activationIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, activationID := range activationIDs {
		if err := cleaner.DeleteActivation(ctx, activationID); err != nil {
			return fmt.Errorf("deleting activation: %w", err)
		}
	}

	return nil
}

func (c *Sweeper) emptyS3PodIdentityBuckets(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewS3Cleaner(c.s3Client, c.logger)
	bucketNames, err := cleaner.ListBuckets(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing buckets: %w", err)
	}

	c.logger.Info("Emptying S3 Pod Identity Buckets", "bucketNames", bucketNames)
	if filterInput.DryRun {
		return nil
	}

	for _, bucketName := range bucketNames {
		if err := cleaner.EmptyS3Bucket(ctx, bucketName); err != nil {
			return fmt.Errorf("emptying bucket %s: %w", bucketName, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupS3PodIdentityBuckets(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewS3Cleaner(c.s3Client, c.logger)
	bucketNames, err := cleaner.ListBuckets(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing buckets: %w", err)
	}

	c.logger.Info("Deleting S3 Pod Identity Buckets", "bucketNames", bucketNames)
	if filterInput.DryRun {
		return nil
	}
	for _, bucketName := range bucketNames {
		if err := cleaner.DeleteBucket(ctx, bucketName); err != nil {
			return fmt.Errorf("deleting bucket %s: %w", bucketName, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupCredentialStacks(ctx context.Context, filterInput FilterInput) error {
	cfnCleaner := NewCFNStackCleanup(c.cfn, c.logger)
	credStacks, err := cfnCleaner.ListCredentialStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing credential stacks: %w", err)
	}

	c.logger.Info("Deleting credential stacks", "credentialStacks", credStacks)
	if filterInput.DryRun {
		return nil
	}
	for _, stack := range credStacks {
		if err := cfnCleaner.DeleteStack(ctx, stack); err != nil {
			return fmt.Errorf("deleting credential stack %s: %w", stack, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupArchitectureStacks(ctx context.Context, filterInput FilterInput) error {
	cfnCleaner := NewCFNStackCleanup(c.cfn, c.logger)
	archStacks, err := cfnCleaner.ListArchitectureStacks(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing architecture stacks: %w", err)
	}

	c.logger.Info("Deleting architecture stacks", "architectureStacks", archStacks)
	if filterInput.DryRun {
		return nil
	}

	for _, stack := range archStacks {
		if err := cfnCleaner.DeleteStack(ctx, stack); err != nil {
			return err
		}
	}
	return nil
}

func (c *Sweeper) cleanupEC2Instances(ctx context.Context, filterInput FilterInput) error {
	ec2Cleaner := NewEC2Cleaner(c.ec2Client, c.logger)
	instanceIDs, err := ec2Cleaner.ListTaggedInstances(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing tagged EC2 instances: %w", err)
	}

	c.logger.Info("Deleting tagged EC2 instances", "instanceIDs", instanceIDs)
	if filterInput.DryRun {
		return nil
	}

	if err := ec2Cleaner.DeleteInstances(ctx, instanceIDs); err != nil {
		return fmt.Errorf("deleting EC2 instances: %w", err)
	}
	return nil
}

func (c *Sweeper) cleanupIAMRoles(ctx context.Context, filterInput FilterInput) error {
	iamCleaner := NewIAMCleaner(c.iam, c.logger)
	roles, err := iamCleaner.ListRoles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing IAM roles: %w", err)
	}

	c.logger.Info("Deleting IAM roles", "roles", roles)
	if filterInput.DryRun {
		return nil
	}

	for _, role := range roles {
		if err := iamCleaner.DeleteRole(ctx, role); err != nil {
			return fmt.Errorf("deleting IAM role %s: %w", role, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupIAMInstanceProfiles(ctx context.Context, filterInput FilterInput) error {
	iamCleaner := NewIAMCleaner(c.iam, c.logger)
	instanceProfiles, err := iamCleaner.ListInstanceProfiles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing IAM instance profiles: %w", err)
	}

	profileRoles := map[string][]string{}
	for _, instanceProfile := range instanceProfiles {
		roles, err := iamCleaner.ListRolesForInstanceProfile(ctx, instanceProfile)
		if err != nil {
			return fmt.Errorf("listing roles for instance profile %s: %w", instanceProfile, err)
		}
		profileRoles[instanceProfile] = roles
	}
	c.logger.Info("Deleting IAM instance profiles", "instanceProfilesToRoles", profileRoles)
	if filterInput.DryRun {
		return nil
	}
	for instanceProfile, roles := range profileRoles {
		if err := iamCleaner.RemoveRolesFromInstanceProfile(ctx, roles, instanceProfile); err != nil {
			return fmt.Errorf("removing roles from instance profile %s: %w", instanceProfile, err)
		}
		if err := iamCleaner.DeleteInstanceProfile(ctx, instanceProfile); err != nil {
			return fmt.Errorf("deleting IAM instance profile %s: %w", instanceProfile, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupEKSClusters(ctx context.Context, filterInput FilterInput) error {
	eksCleaner := NewEKSClusterCleanup(c.eks, c.logger)
	clusterNames, err := eksCleaner.ListEKSClusters(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing EKS hybrid clusters: %w", err)
	}

	c.logger.Info("Deleting EKS hybrid clusters", "clusterNames", clusterNames)
	if filterInput.DryRun {
		return nil
	}

	for _, clusterName := range clusterNames {
		if err := eksCleaner.DeleteCluster(ctx, clusterName); err != nil {
			return fmt.Errorf("deleting EKS hybrid cluster %s: %w", clusterName, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupRolesAnywhereProfiles(ctx context.Context, filterInput FilterInput) error {
	rolesAnywhereCleaner := NewRolesAnywhereCleaner(c.rolesAnywhere, c.logger)

	profiles, err := rolesAnywhereCleaner.ListProfiles(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing Roles Anywhere profiles: %w", err)
	}

	c.logger.Info("Deleting Roles Anywhere profiles", "profiles", profiles)
	if filterInput.DryRun {
		return nil
	}

	for _, profile := range profiles {
		if err := rolesAnywhereCleaner.DeleteProfile(ctx, profile); err != nil {
			return fmt.Errorf("deleting Roles Anywhere profile %s: %w", profile, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupRolesAnywhereTrustAnchors(ctx context.Context, filterInput FilterInput) error {
	rolesAnywhereCleaner := NewRolesAnywhereCleaner(c.rolesAnywhere, c.logger)

	anchors, err := rolesAnywhereCleaner.ListTrustAnchors(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing Roles Anywhere trust anchors: %w", err)
	}

	c.logger.Info("Deleting Roles Anywhere trust anchors", "anchors", anchors)
	if filterInput.DryRun {
		return nil
	}

	for _, anchor := range anchors {
		if err := rolesAnywhereCleaner.DeleteTrustAnchor(ctx, anchor); err != nil {
			return fmt.Errorf("deleting Roles Anywhere trust anchor %s: %w", anchor, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupSSMParameters(ctx context.Context, filterInput FilterInput) error {
	cleaner := NewSSMCleaner(c.ssm, c.logger)

	parameterNames, err := cleaner.ListParameters(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing SSM parameters: %w", err)
	}

	c.logger.Info("Deleting SSM parameters", "parameterNames", parameterNames)
	if filterInput.DryRun {
		return nil
	}
	for _, parameterName := range parameterNames {
		if err := cleaner.DeleteParameter(ctx, parameterName); err != nil {
			return fmt.Errorf("deleting SSM parameter %s: %w", parameterName, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupKeyPairs(ctx context.Context, filterInput FilterInput) error {
	ec2Cleaner := NewEC2Cleaner(c.ec2Client, c.logger)
	keyPairIDs, err := ec2Cleaner.ListKeyPairs(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing key pairs: %w", err)
	}

	c.logger.Info("Deleting key pairs", "keyPairIDs", keyPairIDs)
	if filterInput.DryRun {
		return nil
	}
	for _, keyPairID := range keyPairIDs {
		if err := ec2Cleaner.DeleteKeyPair(ctx, keyPairID); err != nil {
			return fmt.Errorf("deleting key pair %s: %w", keyPairID, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupPeeringConnections(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)
	peeringConnectionIDs, err := vpcCleaner.ListPeeringConnections(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing peering connections: %w", err)
	}

	c.logger.Info("Deleting peering connections", "peeringConnectionIDs", peeringConnectionIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, peeringConnectionID := range peeringConnectionIDs {
		if err := vpcCleaner.DeletePeeringConnection(ctx, peeringConnectionID); err != nil {
			return fmt.Errorf("deleting peering connection %s: %w", peeringConnectionID, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupInternetGateways(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)
	internetGatewayIDs, err := vpcCleaner.ListInternetGateways(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing internet gateways: %w", err)
	}

	c.logger.Info("Deleting internet gateways", "internetGatewayIDs", internetGatewayIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, internetGatewayID := range internetGatewayIDs {
		if err := vpcCleaner.DeleteInternetGateway(ctx, internetGatewayID); err != nil {
			return fmt.Errorf("deleting internet gateway %s: %w", internetGatewayID, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupNetworkInterfaces(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)

	// ENIs are likely not be tagged with our tag, nor should they ever be left around
	// after the instance is deleted, but just in case we attempt to find any orphaned ENIs
	// from VPC IDs
	vpcIDs, err := vpcCleaner.ListVPCs(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing VPCs: %w", err)
	}
	interfaceIds := []string{}

	for _, vpcID := range vpcIDs {
		networkInterfaceIDs, err := vpcCleaner.ListNetworkInterfaces(ctx, vpcID)
		if err != nil {
			return fmt.Errorf("listing network interfaces for VPC %s: %w", vpcID, err)
		}

		interfaceIds = append(interfaceIds, networkInterfaceIDs...)
	}

	c.logger.Info("Deleting network interfaces", "interfaceIds", interfaceIds)
	if filterInput.DryRun {
		return nil
	}

	for _, interfaceID := range interfaceIds {
		if err := vpcCleaner.DeleteNetworkInterface(ctx, interfaceID); err != nil {
			return fmt.Errorf("deleting network interface %s: %w", interfaceID, err)
		}
	}

	return nil
}

func (c *Sweeper) cleanupSubnets(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)
	subnetIDs, err := vpcCleaner.ListSubnets(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing subnets: %w", err)
	}

	c.logger.Info("Deleting subnets", "subnetIDs", subnetIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, subnetID := range subnetIDs {
		if err := vpcCleaner.DeleteSubnet(ctx, subnetID); err != nil {
			return fmt.Errorf("deleting subnet %s: %w", subnetID, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupVPCs(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)
	vpcIDs, err := vpcCleaner.ListVPCs(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing VPCs: %w", err)
	}

	c.logger.Info("Deleting VPCs", "vpcIDs", vpcIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, vpcID := range vpcIDs {
		if err := vpcCleaner.DeleteVPC(ctx, vpcID); err != nil {
			return fmt.Errorf("deleting VPC %s: %w", vpcID, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupRouteTables(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)
	routeTableIDs, err := vpcCleaner.ListRouteTables(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing route tables: %w", err)
	}

	c.logger.Info("Deleting route tables", "routeTableIDs", routeTableIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, routeTableID := range routeTableIDs {
		if err := vpcCleaner.DeleteRouteTable(ctx, routeTableID); err != nil {
			return fmt.Errorf("deleting route table %s: %w", routeTableID, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupSecurityGroups(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)
	securityGroupIDs, err := vpcCleaner.ListSecurityGroups(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing security groups: %w", err)
	}

	c.logger.Info("Deleting security groups", "securityGroupIDs", securityGroupIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, securityGroupID := range securityGroupIDs {
		if err := vpcCleaner.DeleteSecurityGroup(ctx, securityGroupID); err != nil {
			return fmt.Errorf("deleting security group %s: %w", securityGroupID, err)
		}
	}
	return nil
}

func (c *Sweeper) cleanupTransitGateways(ctx context.Context, filterInput FilterInput) error {
	vpcCleaner := NewVPCCleaner(c.ec2Client, c.logger)
	transitGatewayIDs, err := vpcCleaner.ListTransitGateways(ctx, filterInput)
	if err != nil {
		return fmt.Errorf("listing transit gateways: %w", err)
	}

	c.logger.Info("Deleting transit gateways", "transitGatewayIDs", transitGatewayIDs)
	if filterInput.DryRun {
		return nil
	}

	for _, transitGatewayID := range transitGatewayIDs {
		if err := vpcCleaner.DeleteTransitGateway(ctx, transitGatewayID); err != nil {
			return fmt.Errorf("deleting transit gateway %s: %w", transitGatewayID, err)
		}
	}
	return nil
}
