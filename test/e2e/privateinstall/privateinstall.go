// Package privateinstall creates the AWS resources needed to install hybrid nodes
// from private artifacts, close to how an air-gapped customer would set it up.
//
// The node runs in the shared hybrid node subnet, but with its own security group
// that only allows traffic within the test VPCs and to S3 through a gateway endpoint.
// It reaches AWS services through interface endpoints created without private DNS,
// and resolves their hostnames (and the cluster's private API endpoint) through
// /etc/hosts. This keeps the change isolated from other nodes in the VPC.
package privateinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-logr/logr"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/cmd/nodeadm/sync_artifacts"
	awsinternal "github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/cleanup"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/peered"
)

const (
	artifactsPrefix = "artifacts"
	nodeArch        = "amd64"

	endpointAvailableTimeout = 10 * time.Minute
)

// interfaceEndpointServices are the AWS services a hybrid node calls during
// install, init and normal operation.
var interfaceEndpointServices = []string{
	"ssm",
	"ssmmessages",
	"ec2messages",
	"sts",
	"ecr.api",
	"ecr.dkr",
	"eks",
	"rolesanywhere",
}

// Config is the input for creating a private install environment.
type Config struct {
	EC2    *ec2.Client
	S3     *s3.Client
	EKS    *eks.Client
	Logger logr.Logger

	Cluster   *peered.HybridCluster
	DNSSuffix string
	// KubernetesVersion is the version of the artifacts to sync.
	KubernetesVersion string
	// NodeadmURLs are the URLs of the nodeadm binaries under test. The amd64 binary
	// is copied to the private bucket so the node can download it without internet access.
	NodeadmURLs e2e.NodeadmURLs
	// EcrAccounts are the ECR registries, other than the EKS registry for the region,
	// that nodes pull images from.
	EcrAccounts []string
	// ArtifactsDir is a local directory for the generated manifest.
	ArtifactsDir string
	// ReleaseManifestURL is the nodeadm release manifest to sync artifacts from.
	// Defaults to the public manifest for the cluster's partition. nodeadm sets this
	// at build time, which test binaries don't.
	ReleaseManifestURL string
}

const (
	defaultReleaseManifestURL = "https://hybrid-assets.eks.amazonaws.com/manifest.yaml"
	chinaReleaseManifestURL   = "https://eks-hybrid-assets.awsstatic.cn/manifest.yaml"
)

func releaseManifestURL(cfg Config) string {
	if cfg.ReleaseManifestURL != "" {
		return cfg.ReleaseManifestURL
	}
	if awsinternal.GetPartitionFromRegionFallback(cfg.Cluster.Region) == "aws-cn" {
		return chinaReleaseManifestURL
	}
	return defaultReleaseManifestURL
}

// Environment holds the resources created for private installation.
type Environment struct {
	BucketName        string
	GatewayEndpointID string
	// ownsGatewayEndpoint is true if this environment created the gateway endpoint.
	ownsGatewayEndpoint     bool
	InterfaceEndpointIDs    []string
	EndpointSecurityGroupID string
	NodeSecurityGroupID     string

	NodeadmURLs  e2e.NodeadmURLs
	ManifestURL  string
	HostsEntries []e2e.HostsEntry
}

// NodeSpec returns the node configuration for installing from this environment.
func (e *Environment) NodeSpec() *peered.PrivateInstallSpec {
	return &peered.PrivateInstallSpec{
		SecurityGroupID: e.NodeSecurityGroupID,
		NodeadmURLs:     e.NodeadmURLs,
		ManifestURL:     e.ManifestURL,
		HostsEntries:    e.HostsEntries,
	}
}

type network struct {
	vpcID           string
	vpcCIDR         string
	subnetID        string
	routeTableID    string
	clusterVPCID    string
	clusterVPCCIDR  string
	podCIDRs        []string
	apiServerHost   string
	clusterEndpoint string
}

// Setup creates the private install environment. On error, the returned environment
// contains whatever was created so the caller can pass it to Cleanup.
func Setup(ctx context.Context, cfg Config) (*Environment, error) {
	env := &Environment{}
	region := cfg.Cluster.Region

	net, err := discoverNetwork(ctx, cfg)
	if err != nil {
		return env, err
	}

	services, err := findEndpointServices(ctx, cfg.EC2, region, append([]string{"s3"}, interfaceEndpointServices...))
	if err != nil {
		return env, err
	}
	s3Service, ok := services["s3"]
	if !ok {
		return env, fmt.Errorf("s3 gateway endpoint service not found in %s", region)
	}

	tags := resourceTags(cfg.Cluster.Name)

	// A route table can only have one S3 gateway endpoint. Reuse an existing one, for
	// example from a previous run with SKIP_CLEANUP, and only delete it if this run created it.
	existingGateway, err := findGatewayEndpoint(ctx, cfg.EC2, net.vpcID, net.routeTableID, aws.ToString(s3Service.ServiceName))
	if err != nil {
		return env, err
	}
	if existingGateway != "" {
		cfg.Logger.Info("Reusing existing S3 gateway endpoint for the hybrid node route table", "routeTableID", net.routeTableID, "vpcEndpointID", existingGateway)
		env.GatewayEndpointID = existingGateway
	} else {
		cfg.Logger.Info("Creating S3 gateway endpoint for the hybrid node route table", "routeTableID", net.routeTableID)
		gateway, err := cfg.EC2.CreateVpcEndpoint(ctx, &ec2.CreateVpcEndpointInput{
			VpcId:             aws.String(net.vpcID),
			ServiceName:       s3Service.ServiceName,
			VpcEndpointType:   ec2types.VpcEndpointTypeGateway,
			RouteTableIds:     []string{net.routeTableID},
			TagSpecifications: []ec2types.TagSpecification{{ResourceType: ec2types.ResourceTypeVpcEndpoint, Tags: tags}},
		})
		if err != nil {
			return env, fmt.Errorf("creating S3 gateway endpoint: %w", err)
		}
		env.GatewayEndpointID = aws.ToString(gateway.VpcEndpoint.VpcEndpointId)
		env.ownsGatewayEndpoint = true
	}

	endpointSG, err := createSecurityGroup(ctx, cfg.EC2, net.vpcID, cfg.Cluster.Name+"-private-install-endpoints", "Interface endpoints for private install e2e tests", tags)
	if err != nil {
		return env, err
	}
	env.EndpointSecurityGroupID = endpointSG
	if _, err := cfg.EC2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(endpointSG),
		IpPermissions: []ec2types.IpPermission{tcpPermission(443, net.vpcCIDR)},
	}); err != nil {
		return env, fmt.Errorf("authorizing endpoint security group ingress: %w", err)
	}

	for _, name := range interfaceEndpointServices {
		service, ok := services[name]
		if !ok {
			cfg.Logger.Info("Interface endpoint service not available in region, skipping", "service", name, "region", region)
			continue
		}
		cfg.Logger.Info("Creating interface endpoint", "service", aws.ToString(service.ServiceName))
		endpoint, err := cfg.EC2.CreateVpcEndpoint(ctx, &ec2.CreateVpcEndpointInput{
			VpcId:             aws.String(net.vpcID),
			ServiceName:       service.ServiceName,
			VpcEndpointType:   ec2types.VpcEndpointTypeInterface,
			SubnetIds:         []string{net.subnetID},
			SecurityGroupIds:  []string{endpointSG},
			PrivateDnsEnabled: aws.Bool(false),
			TagSpecifications: []ec2types.TagSpecification{{ResourceType: ec2types.ResourceTypeVpcEndpoint, Tags: tags}},
		})
		if err != nil {
			return env, fmt.Errorf("creating %s interface endpoint: %w", name, err)
		}
		env.InterfaceEndpointIDs = append(env.InterfaceEndpointIDs, aws.ToString(endpoint.VpcEndpoint.VpcEndpointId))
	}

	nodeSG, err := createNodeSecurityGroup(ctx, cfg, net, aws.ToString(s3Service.ServiceName), tags)
	if nodeSG != "" {
		env.NodeSecurityGroupID = nodeSG
	}
	if err != nil {
		return env, err
	}

	manifestSource := releaseManifestURL(cfg)
	source, err := awsinternal.GetLatestSourceFromManifest(ctx, cfg.KubernetesVersion, region, manifestSource)
	if err != nil {
		return env, fmt.Errorf("getting latest nodeadm source for %s from %s: %w", cfg.KubernetesVersion, manifestSource, err)
	}

	bucketName := constants.PrivateInstallBucketPrefix + randomSuffix(12)
	if err := createBucket(ctx, cfg, bucketName, region, env.GatewayEndpointID); err != nil {
		return env, err
	}
	env.BucketName = bucketName
	bucketURL := fmt.Sprintf("https://%s.s3.%s.%s", bucketName, region, cfg.DNSSuffix)

	manifestURL, err := syncArtifacts(ctx, cfg, source, bucketName, bucketURL)
	if err != nil {
		return env, err
	}
	env.ManifestURL = manifestURL

	nodeadmURL, err := copyNodeadm(ctx, cfg, bucketName, bucketURL)
	if err != nil {
		return env, err
	}
	env.NodeadmURLs = e2e.NodeadmURLs{AMD: nodeadmURL}

	if err := waitForEndpointsAvailable(ctx, cfg.EC2, append([]string{env.GatewayEndpointID}, env.InterfaceEndpointIDs...)); err != nil {
		return env, err
	}

	registries := append([]string{source.RegionInfo.EcrAccountID}, cfg.EcrAccounts...)
	hosts, err := endpointHostsEntries(ctx, cfg.EC2, env.InterfaceEndpointIDs, registries)
	if err != nil {
		return env, err
	}
	apiHosts, err := apiServerHostsEntries(ctx, cfg, net)
	if err != nil {
		return env, err
	}
	env.HostsEntries = append(hosts, apiHosts...)

	cfg.Logger.Info("Private install environment ready", "bucket", bucketName, "manifestURL", manifestURL, "hostsEntries", env.HostsEntries)
	return env, nil
}

// Cleanup deletes the resources in env. It's safe to call with a partially created environment.
func Cleanup(ctx context.Context, cfg Config, env *Environment) error {
	if env == nil {
		return nil
	}
	vpcCleaner := cleanup.NewVPCCleaner(cfg.EC2, cfg.Logger)
	var errs []error

	if env.BucketName != "" {
		if err := cleanup.NewS3Cleaner(cfg.S3, cfg.Logger).DeleteBucket(ctx, env.BucketName); err != nil {
			errs = append(errs, err)
		}
	}

	endpointIDs := env.InterfaceEndpointIDs
	if env.GatewayEndpointID != "" && env.ownsGatewayEndpoint {
		endpointIDs = append(endpointIDs, env.GatewayEndpointID)
	}
	if err := vpcCleaner.DeleteVPCEndpointsAndWait(ctx, endpointIDs); err != nil {
		errs = append(errs, err)
	}

	for _, sg := range []string{env.NodeSecurityGroupID, env.EndpointSecurityGroupID} {
		if sg == "" {
			continue
		}
		if err := vpcCleaner.DeleteSecurityGroupWithRetry(ctx, sg); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleaning up private install environment: %v", errs)
	}
	return nil
}

func discoverNetwork(ctx context.Context, cfg Config) (*network, error) {
	net := &network{subnetID: cfg.Cluster.SubnetID}

	subnets, err := cfg.EC2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{SubnetIds: []string{net.subnetID}})
	if err != nil || len(subnets.Subnets) == 0 {
		return nil, fmt.Errorf("describing hybrid node subnet %s: %w", net.subnetID, err)
	}
	net.vpcID = aws.ToString(subnets.Subnets[0].VpcId)

	vpcs, err := cfg.EC2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{net.vpcID}})
	if err != nil || len(vpcs.Vpcs) == 0 {
		return nil, fmt.Errorf("describing hybrid node VPC %s: %w", net.vpcID, err)
	}
	net.vpcCIDR = aws.ToString(vpcs.Vpcs[0].CidrBlock)

	routeTables, err := cfg.EC2.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{{Name: aws.String("association.subnet-id"), Values: []string{net.subnetID}}},
	})
	if err != nil || len(routeTables.RouteTables) == 0 {
		return nil, fmt.Errorf("finding route table for hybrid node subnet %s: %w", net.subnetID, err)
	}
	net.routeTableID = aws.ToString(routeTables.RouteTables[0].RouteTableId)

	cluster, err := cfg.EKS.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(cfg.Cluster.Name)})
	if err != nil {
		return nil, fmt.Errorf("describing cluster %s: %w", cfg.Cluster.Name, err)
	}
	vpcConfig := cluster.Cluster.ResourcesVpcConfig
	if vpcConfig == nil || !vpcConfig.EndpointPrivateAccess {
		return nil, fmt.Errorf("cluster %s must have private endpoint access enabled; set endpointPrivateAccess: true in the setup config", cfg.Cluster.Name)
	}
	net.clusterVPCID = aws.ToString(vpcConfig.VpcId)
	net.clusterEndpoint = aws.ToString(cluster.Cluster.Endpoint)
	endpointURL, err := url.Parse(net.clusterEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing cluster endpoint %s: %w", net.clusterEndpoint, err)
	}
	net.apiServerHost = endpointURL.Hostname()

	clusterVPCs, err := cfg.EC2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{net.clusterVPCID}})
	if err != nil || len(clusterVPCs.Vpcs) == 0 {
		return nil, fmt.Errorf("describing cluster VPC %s: %w", net.clusterVPCID, err)
	}
	net.clusterVPCCIDR = aws.ToString(clusterVPCs.Vpcs[0].CidrBlock)

	if remote := cluster.Cluster.RemoteNetworkConfig; remote != nil {
		for _, podNetwork := range remote.RemotePodNetworks {
			net.podCIDRs = append(net.podCIDRs, podNetwork.Cidrs...)
		}
	}

	return net, nil
}

// findGatewayEndpoint returns the ID of an available gateway endpoint for the service
// that's associated with the route table, or "" if there isn't one.
func findGatewayEndpoint(ctx context.Context, client *ec2.Client, vpcID, routeTableID, serviceName string) (string, error) {
	output, err := client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("service-name"), Values: []string{serviceName}},
			{Name: aws.String("vpc-endpoint-type"), Values: []string{string(ec2types.VpcEndpointTypeGateway)}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("describing S3 gateway endpoints: %w", err)
	}
	for _, endpoint := range output.VpcEndpoints {
		if !strings.EqualFold(string(endpoint.State), "available") {
			continue
		}
		for _, id := range endpoint.RouteTableIds {
			if id == routeTableID {
				return aws.ToString(endpoint.VpcEndpointId), nil
			}
		}
	}
	return "", nil
}

// findEndpointServices returns the VPC endpoint service details for the given short
// service names (for example "ecr.dkr"), keyed by short name. Service names have a
// partition-specific prefix, so both the commercial and China forms are queried.
func findEndpointServices(ctx context.Context, client *ec2.Client, region string, names []string) (map[string]ec2types.ServiceDetail, error) {
	candidates := map[string]string{}
	var serviceNames []string
	for _, name := range names {
		for _, prefix := range []string{"com.amazonaws.", "cn.com.amazonaws."} {
			serviceName := prefix + region + "." + name
			candidates[serviceName] = name
			serviceNames = append(serviceNames, serviceName)
		}
	}

	services := map[string]ec2types.ServiceDetail{}
	input := &ec2.DescribeVpcEndpointServicesInput{
		Filters: []ec2types.Filter{{Name: aws.String("service-name"), Values: serviceNames}},
	}
	for {
		output, err := client.DescribeVpcEndpointServices(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("describing VPC endpoint services: %w", err)
		}
		for _, detail := range output.ServiceDetails {
			name, ok := candidates[aws.ToString(detail.ServiceName)]
			if !ok {
				continue
			}
			// The s3 service has both gateway and interface types. Use the gateway type for s3
			// and the interface type for everything else.
			wantType := ec2types.ServiceTypeInterface
			if name == "s3" {
				wantType = ec2types.ServiceTypeGateway
			}
			for _, serviceType := range detail.ServiceType {
				if serviceType.ServiceType == wantType {
					services[name] = detail
				}
			}
		}
		if output.NextToken == nil {
			break
		}
		input.NextToken = output.NextToken
	}
	return services, nil
}

func createNodeSecurityGroup(ctx context.Context, cfg Config, net *network, s3ServiceName string, tags []ec2types.Tag) (string, error) {
	sg, err := createSecurityGroup(ctx, cfg.EC2, net.vpcID, cfg.Cluster.Name+"-private-install-nodes", "Hybrid nodes without internet access for private install e2e tests", tags)
	if err != nil {
		return "", err
	}

	// Remove the default allow-all egress rule so the node can't reach the internet.
	if _, err := cfg.EC2.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
		GroupId: aws.String(sg),
		IpPermissions: []ec2types.IpPermission{{
			IpProtocol: aws.String("-1"),
			IpRanges:   []ec2types.IpRange{{CidrIp: aws.String("0.0.0.0/0")}},
		}},
	}); err != nil {
		return sg, fmt.Errorf("revoking default egress from node security group: %w", err)
	}

	// Allow all traffic within the hybrid node VPC (jumpbox, other nodes, interface
	// endpoints), the cluster VPC (control plane) and the pod network.
	cidrs := append([]string{net.vpcCIDR, net.clusterVPCCIDR}, net.podCIDRs...)
	var permissions []ec2types.IpPermission
	for _, cidr := range cidrs {
		permissions = append(permissions, ec2types.IpPermission{
			IpProtocol: aws.String("-1"),
			IpRanges:   []ec2types.IpRange{{CidrIp: aws.String(cidr)}},
		})
	}
	if _, err := cfg.EC2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(sg),
		IpPermissions: permissions,
	}); err != nil {
		return sg, fmt.Errorf("authorizing node security group ingress: %w", err)
	}

	prefixLists, err := cfg.EC2.DescribePrefixLists(ctx, &ec2.DescribePrefixListsInput{
		Filters: []ec2types.Filter{{Name: aws.String("prefix-list-name"), Values: []string{s3ServiceName}}},
	})
	if err != nil || len(prefixLists.PrefixLists) == 0 {
		return sg, fmt.Errorf("finding S3 prefix list %s: %w", s3ServiceName, err)
	}
	egress := append(permissions, ec2types.IpPermission{
		IpProtocol:    aws.String("tcp"),
		FromPort:      aws.Int32(443),
		ToPort:        aws.Int32(443),
		PrefixListIds: []ec2types.PrefixListId{{PrefixListId: prefixLists.PrefixLists[0].PrefixListId}},
	})
	if _, err := cfg.EC2.AuthorizeSecurityGroupEgress(ctx, &ec2.AuthorizeSecurityGroupEgressInput{
		GroupId:       aws.String(sg),
		IpPermissions: egress,
	}); err != nil {
		return sg, fmt.Errorf("authorizing node security group egress: %w", err)
	}

	return sg, nil
}

func createSecurityGroup(ctx context.Context, client *ec2.Client, vpcID, name, description string, tags []ec2types.Tag) (string, error) {
	output, err := client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:         aws.String(name + "-" + randomSuffix(6)),
		Description:       aws.String(description),
		VpcId:             aws.String(vpcID),
		TagSpecifications: []ec2types.TagSpecification{{ResourceType: ec2types.ResourceTypeSecurityGroup, Tags: tags}},
	})
	if err != nil {
		return "", fmt.Errorf("creating security group %s: %w", name, err)
	}
	return aws.ToString(output.GroupId), nil
}

func tcpPermission(port int32, cidr string) ec2types.IpPermission {
	return ec2types.IpPermission{
		IpProtocol: aws.String("tcp"),
		FromPort:   aws.Int32(port),
		ToPort:     aws.Int32(port),
		IpRanges:   []ec2types.IpRange{{CidrIp: aws.String(cidr)}},
	}
}

// createBucket creates the artifacts bucket with ACLs disabled and Block Public Access
// enabled (the S3 defaults), and the bucket policy from the private installation guide.
func createBucket(ctx context.Context, cfg Config, bucketName, region, vpcEndpointID string) error {
	input := &s3.CreateBucketInput{Bucket: aws.String(bucketName)}
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	cfg.Logger.Info("Creating private artifacts bucket", "bucket", bucketName)
	if _, err := cfg.S3.CreateBucket(ctx, input); err != nil {
		return fmt.Errorf("creating bucket %s: %w", bucketName, err)
	}

	if _, err := cfg.S3.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: aws.String(bucketName),
		Tagging: &s3types.Tagging{TagSet: []s3types.Tag{
			{Key: aws.String(constants.TestClusterTagKey), Value: aws.String(cfg.Cluster.Name)},
		}},
	}); err != nil {
		return fmt.Errorf("tagging bucket %s: %w", bucketName, err)
	}

	// Set the defaults explicitly so the test doesn't depend on account or partition defaults.
	if _, err := cfg.S3.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{
		Bucket: aws.String(bucketName),
		OwnershipControls: &s3types.OwnershipControls{Rules: []s3types.OwnershipControlsRule{
			{ObjectOwnership: s3types.ObjectOwnershipBucketOwnerEnforced},
		}},
	}); err != nil {
		return fmt.Errorf("disabling ACLs on bucket %s: %w", bucketName, err)
	}
	if _, err := cfg.S3.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: aws.String(bucketName),
		PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(true),
			BlockPublicPolicy:     aws.Bool(true),
			IgnorePublicAcls:      aws.Bool(true),
			RestrictPublicBuckets: aws.Bool(true),
		},
	}); err != nil {
		return fmt.Errorf("blocking public access on bucket %s: %w", bucketName, err)
	}

	policy, err := bucketPolicy(bucketName, region, vpcEndpointID)
	if err != nil {
		return err
	}
	if _, err := cfg.S3.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucketName),
		Policy: aws.String(policy),
	}); err != nil {
		return fmt.Errorf("putting bucket policy on %s: %w", bucketName, err)
	}
	return nil
}

// bucketPolicy matches the policy in the private installation guide: unauthenticated
// reads are allowed only through the VPC endpoint.
func bucketPolicy(bucketName, region, vpcEndpointID string) (string, error) {
	partition := awsinternal.GetPartitionFromRegionFallback(region)
	resource := fmt.Sprintf("arn:%s:s3:::%s/*", partition, bucketName)
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Sid":       "AllowGetObjectFromVpce",
				"Effect":    "Allow",
				"Principal": "*",
				"Action":    "s3:GetObject",
				"Resource":  resource,
				"Condition": map[string]any{"StringEquals": map[string]string{"aws:SourceVpce": vpcEndpointID}},
			},
			{
				"Sid":       "DenyGetObjectOutsideVpce",
				"Effect":    "Deny",
				"Principal": "*",
				"Action":    "s3:GetObject",
				"Resource":  resource,
				"Condition": map[string]any{"StringNotEquals": map[string]string{"aws:SourceVpce": vpcEndpointID}},
			},
		},
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("marshaling bucket policy: %w", err)
	}
	return string(data), nil
}

// syncArtifacts runs the same code as `nodeadm sync-artifacts` and uploads the generated
// manifest to the bucket. It returns the manifest URL.
func syncArtifacts(ctx context.Context, cfg Config, source awsinternal.Source, bucketName, bucketURL string) (string, error) {
	outputDir, err := os.MkdirTemp(cfg.ArtifactsDir, "private-install-manifest-")
	if err != nil {
		return "", fmt.Errorf("creating manifest directory: %w", err)
	}

	logger, err := zap.NewDevelopment()
	if err != nil {
		return "", fmt.Errorf("creating sync logger: %w", err)
	}
	downloader := &sync_artifacts.Downloader{
		AwsSource:     source,
		Arch:          nodeArch,
		OS:            "linux",
		Region:        cfg.Cluster.Region,
		S3Bucket:      bucketName,
		S3Prefix:      artifactsPrefix,
		Logger:        logger,
		S3Client:      cfg.S3,
		SyncTimestamp: time.Now().Unix(),
		OutputDir:     outputDir,
	}
	cfg.Logger.Info("Syncing nodeadm artifacts to private bucket", "bucket", bucketName, "version", source.Eks.Version)
	if err := downloader.Run(ctx); err != nil {
		return "", fmt.Errorf("syncing artifacts: %w", err)
	}

	manifest, err := os.Open(downloader.ManifestPath)
	if err != nil {
		return "", fmt.Errorf("opening manifest %s: %w", downloader.ManifestPath, err)
	}
	defer manifest.Close()

	key := "manifests/" + filepath.Base(downloader.ManifestPath)
	if _, err := manager.NewUploader(cfg.S3).Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   manifest,
	}); err != nil {
		return "", fmt.Errorf("uploading manifest: %w", err)
	}
	return bucketURL + "/" + key, nil
}

// copyNodeadm copies the nodeadm binary under test to the bucket and returns its URL.
func copyNodeadm(ctx context.Context, cfg Config, bucketName, bucketURL string) (string, error) {
	if cfg.NodeadmURLs.AMD == "" {
		return "", fmt.Errorf("an amd64 nodeadm URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.NodeadmURLs.AMD, nil)
	if err != nil {
		return "", fmt.Errorf("creating nodeadm download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading nodeadm: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading nodeadm: unexpected status %s", resp.Status)
	}

	key := "nodeadm/" + nodeArch + "/nodeadm"
	if _, err := manager.NewUploader(cfg.S3).Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   resp.Body,
	}); err != nil {
		return "", fmt.Errorf("uploading nodeadm: %w", err)
	}
	return bucketURL + "/" + key, nil
}

func waitForEndpointsAvailable(ctx context.Context, client *ec2.Client, endpointIDs []string) error {
	ctx, cancel := context.WithTimeout(ctx, endpointAvailableTimeout)
	defer cancel()
	for {
		output, err := client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{VpcEndpointIds: endpointIDs})
		if err != nil {
			return fmt.Errorf("describing VPC endpoints: %w", err)
		}
		pending := 0
		for _, endpoint := range output.VpcEndpoints {
			switch strings.ToLower(string(endpoint.State)) {
			case "available":
			case "failed", "rejected", "deleted", "deleting":
				return fmt.Errorf("VPC endpoint %s is %s", aws.ToString(endpoint.VpcEndpointId), endpoint.State)
			default:
				pending++
			}
		}
		if pending == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for VPC endpoints to be available: %w", ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}

// endpointHostsEntries maps the private DNS names of the interface endpoints to the
// endpoints' private IPs. Wildcard ECR registry names are expanded for each registry.
func endpointHostsEntries(ctx context.Context, client *ec2.Client, endpointIDs, registries []string) ([]e2e.HostsEntry, error) {
	if len(endpointIDs) == 0 {
		return nil, nil
	}
	endpoints, err := client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{VpcEndpointIds: endpointIDs})
	if err != nil {
		return nil, fmt.Errorf("describing interface endpoints: %w", err)
	}

	var serviceNames []string
	for _, endpoint := range endpoints.VpcEndpoints {
		serviceNames = append(serviceNames, aws.ToString(endpoint.ServiceName))
	}
	services, err := client.DescribeVpcEndpointServices(ctx, &ec2.DescribeVpcEndpointServicesInput{ServiceNames: serviceNames})
	if err != nil {
		return nil, fmt.Errorf("describing interface endpoint services: %w", err)
	}
	dnsNames := map[string][]string{}
	for _, service := range services.ServiceDetails {
		var names []string
		for _, name := range service.PrivateDnsNames {
			names = append(names, aws.ToString(name.PrivateDnsName))
		}
		if len(names) == 0 && service.PrivateDnsName != nil {
			names = append(names, aws.ToString(service.PrivateDnsName))
		}
		dnsNames[aws.ToString(service.ServiceName)] = names
	}

	var entries []e2e.HostsEntry
	for _, endpoint := range endpoints.VpcEndpoints {
		if len(endpoint.NetworkInterfaceIds) == 0 {
			return nil, fmt.Errorf("interface endpoint %s has no network interfaces", aws.ToString(endpoint.VpcEndpointId))
		}
		enis, err := client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: endpoint.NetworkInterfaceIds})
		if err != nil || len(enis.NetworkInterfaces) == 0 {
			return nil, fmt.Errorf("describing network interfaces for endpoint %s: %w", aws.ToString(endpoint.VpcEndpointId), err)
		}
		ip := aws.ToString(enis.NetworkInterfaces[0].PrivateIpAddress)

		for _, name := range dnsNames[aws.ToString(endpoint.ServiceName)] {
			if !strings.HasPrefix(name, "*.") {
				entries = append(entries, e2e.HostsEntry{IP: ip, Hostname: name})
				continue
			}
			// Only the ECR registry wildcard (*.dkr.ecr...) is expanded; nodes pull from a
			// known set of registries.
			if !strings.Contains(name, ".dkr.ecr") {
				continue
			}
			for _, registry := range registries {
				if registry == "" {
					continue
				}
				entries = append(entries, e2e.HostsEntry{IP: ip, Hostname: registry + strings.TrimPrefix(name, "*")})
			}
		}
	}
	return entries, nil
}

// apiServerHostsEntries maps the cluster API server hostname to the private IPs of the
// control plane network interfaces in the cluster VPC.
func apiServerHostsEntries(ctx context.Context, cfg Config, net *network) ([]e2e.HostsEntry, error) {
	enis, err := cfg.EC2.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{net.clusterVPCID}},
			{Name: aws.String("description"), Values: []string{"Amazon EKS " + cfg.Cluster.Name}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describing cluster network interfaces: %w", err)
	}
	var entries []e2e.HostsEntry
	for _, eni := range enis.NetworkInterfaces {
		entries = append(entries, e2e.HostsEntry{IP: aws.ToString(eni.PrivateIpAddress), Hostname: net.apiServerHost})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no control plane network interfaces found for cluster %s", cfg.Cluster.Name)
	}
	return entries, nil
}

func resourceTags(clusterName string) []ec2types.Tag {
	return []ec2types.Tag{
		{Key: aws.String(constants.TestClusterTagKey), Value: aws.String(clusterName)},
		{Key: aws.String(constants.CreationTimeTagKey), Value: aws.String(time.Now().Format(time.RFC3339))},
		{Key: aws.String(constants.PrivateInstallTagKey), Value: aws.String("true")},
	}
}

const suffixChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomSuffix(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = suffixChars[rand.IntN(len(suffixChars))]
	}
	return string(b)
}
