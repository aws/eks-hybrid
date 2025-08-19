import * as cdk from 'aws-cdk-lib';
import * as iam from 'aws-cdk-lib/aws-iam';

export function createNodeadmTestsCreationCleanupPolicy(
  stack: cdk.Stack,
  testClusterTagKey: string,
  testClusterPrefix: string,
  binaryBucketArn: string,
  podIdentityS3BucketPrefix: string,
): iam.ManagedPolicy[] {
  const requestTagCondition = {
    StringLike: {
      [`aws:RequestTag/${testClusterTagKey}`]: `${testClusterPrefix}-*`,
    },
  };
  const resourceTagCondition = {
    StringLike: {
      [`aws:ResourceTag/${testClusterTagKey}`]: `${testClusterPrefix}-*`,
    },
  };

 
  const iamPolicy = new iam.ManagedPolicy(stack, 'nodeadm-e2e-tests-iam-policy', {
    statements: [
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['cloudformation:CreateChangeSet', 'cloudformation:ExecuteChangeSet'],
        resources: ['arn:aws:cloudformation:*:aws:transform/LanguageExtensions'],
      }),
      new iam.PolicyStatement({
        actions: [
          'iam:AttachRolePolicy',
          'iam:DetachRolePolicy',
          'iam:GetRole',
          'iam:GetRolePolicy',
          'iam:ListRoles',
          'iam:ListRoleTags',
          'iam:PassRole',
          'iam:PutRolePolicy',
          'iam:TagRole',
        ],
        resources: [`arn:aws:iam::${stack.account}:role/*`],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: [
          'iam:CreateRole',
          'iam:DeleteRole',
          'iam:DeleteRolePolicy',
          'iam:ListAttachedRolePolicies',
          'iam:ListInstanceProfilesForRole',
          'iam:ListRolePolicies',
        ],
        resources: [`arn:aws:iam::${stack.account}:role/*`],
        effect: iam.Effect.ALLOW,
        conditions: resourceTagCondition,
      }),
      new iam.PolicyStatement({
        actions: ['iam:CreateServiceLinkedRole'],
        resources: [`arn:aws:iam::${stack.account}:role/aws-service-role/*`],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: [
          'iam:AddRoleToInstanceProfile',
          'iam:CreateInstanceProfile',
          'iam:DeleteInstanceProfile',
          'iam:GetInstanceProfile',
          'iam:ListInstanceProfiles',
          'iam:RemoveRoleFromInstanceProfile',
        ],
        resources: [`arn:aws:iam::${stack.account}:instance-profile/*`],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: ['iam:TagInstanceProfile'],
        resources: [`arn:aws:iam::${stack.account}:instance-profile/*`],
        effect: iam.Effect.ALLOW,
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: [
          'iam:ListOpenIDConnectProviders',
          'iam:CreateOpenIDConnectProvider',
          'iam:UpdateAssumeRolePolicy',
        ],
        resources: [
          `arn:aws:iam::${stack.account}:oidc-provider/*`,
          `arn:aws:iam::${stack.account}:role/*CloudWatch*`,
        ],
      }),
    ],
  });

  const ec2Policy = new iam.ManagedPolicy(stack, 'nodeadm-e2e-tests-ec2-policy', {
    statements: [
      new iam.PolicyStatement({
        actions: [
          'ec2:DescribeInstances',
          'ec2:DescribeInstanceTypes',
          'ec2:DescribeInstanceAttribute',
          'ec2:DescribeInstanceStatus',
          'ec2:DescribeSecurityGroups',
          'ec2:DescribeSubnets',
          'ec2:DescribeVpcs',
          'ec2:DescribeRouteTables',
          'ec2:DescribeRoutes',
          'ec2:DescribeTransitGateways',
          'ec2:DescribeTransitGatewayAttachments',
          'ec2:DescribeTransitGatewayRouteTables',
          'ec2:DescribeTransitGatewayVpcAttachments',
          'ec2:DescribeVpcPeeringConnections',
          'ec2:DescribeLaunchTemplates',
          'ec2:DescribeLaunchTemplateVersions',
          'ec2:DescribeFleets',
          'ec2:DescribeNetworkInterfaces',
          'ec2:DescribeSnapshots',
          'ec2:DescribeImages',
          'ec2:DescribeKeyPairs',
          'ec2:DescribeAvailabilityZones',
          'ec2:DescribeRegions',
          'ec2:DescribeInternetGateways',
          'ec2:DescribeNatGateways',
          'ec2:DescribeVpcEndpoints',
          'ec2:DescribeTags',
        ],
        resources: ['*'],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: [
          'ec2:CreateFleet',
          'ec2:CreateLaunchTemplate',
          'ec2:CreateLaunchTemplateVersion',
          'ec2:CreateRoute',
          'ec2:CreateRouteTable',
          'ec2:CreateSubnet',
          'ec2:CreateTransitGateway',
          'ec2:CreateTransitGatewayRoute',
          'ec2:CreateTransitGatewayRouteTable',
          'ec2:CreateTransitGatewayVpcAttachment',
          'ec2:CreateVpcPeeringConnection',
          'ec2:CreateSecurityGroup',
          'ec2:CreateNetworkInterface',
          'ec2:CreateSnapshot',
          'ec2:CreateImage',
          'ec2:CreateNatGateway',
          'ec2:CreateVpcEndpoint',
        ],
        resources: ['*'],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: [
          'ec2:DeleteSecurityGroup',
          'ec2:DeleteRoute',
          'ec2:DeleteRouteTable',
          'ec2:DeleteSubnet',
          'ec2:DeleteTransitGateway',
          'ec2:DeleteTransitGatewayRoute',
          'ec2:DeleteTransitGatewayRouteTable',
          'ec2:DeleteTransitGatewayVpcAttachment',
          'ec2:DeleteVpcPeeringConnection',
          'ec2:DeleteLaunchTemplate',
          'ec2:DeleteLaunchTemplateVersion',
          'ec2:DeleteFleets',
          'ec2:DeleteKeyPair',
          'ec2:DeleteNetworkInterface',
          'ec2:DeleteSnapshot',
          'ec2:DeleteImage',
          'ec2:DeleteInternetGateway',
          'ec2:DeleteVpc',
          'ec2:DeleteNatGateway',
          'ec2:DeleteVpcEndpoint',
        ],
        resources: ['*'],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: ['ec2:CreateInternetGateway', 'ec2:CreateKeyPair', 'ec2:CreateTags', 'ec2:CreateVpc'],
        resources: ['*'],
        effect: iam.Effect.ALLOW,
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        actions: [
          'ec2:DetachInternetGateway',
          'ec2:DisassociateRouteTable',
          'ec2:RebootInstances',
          'ec2:StopInstances',
          'ec2:TerminateInstances',
          'ec2-instance-connect:SendSerialConsoleSSHPublicKey',
        ],
        resources: ['*'],
        effect: iam.Effect.ALLOW,
        conditions: resourceTagCondition,
      }),
    ],
  });

  const servicesPolicy = new iam.ManagedPolicy(stack, 'nodeadm-e2e-tests-services-policy', {
    statements: [
      new iam.PolicyStatement({
        actions: [
          'cloudformation:ListStacks',
          'cloudformation:DescribeStacks',
          'ec2:AcceptVpcPeeringConnection',
          'ec2:AssociateRouteTable',
          'ec2:AssociateTransitGatewayRouteTable',
          'ec2:AttachInternetGateway',
          'ec2:AuthorizeSecurityGroupIngress',
          'ec2:DisassociateTransitGatewayRouteTable',
          'ec2:GetLaunchTemplateData',
          'ec2:GetTransitGatewayRouteTableAssociations',
          'ec2:ModifyFleet',
          'ec2:ModifyInstanceAttribute',
          'ec2:ModifySubnetAttribute',
          'ec2:ModifyVpcAttribute',
          'ec2:RevokeSecurityGroupIngress',
          'ec2:RunInstances',
          'ec2:SearchTransitGatewayRoutes',
          'logs:DescribeLogGroups',
          'rolesanywhere:ListTrustAnchors',
          'rolesanywhere:ListProfiles',
          'ssm:DeleteActivation',
          'ssm:DeleteParameter',
          'ssm:DescribeActivations',
          'ssm:DescribeInstanceInformation',
          'ssm:DescribeInstanceInformation',
          'ssm:DescribeParameters',
          'ssm:GetParameters',
          'ssm:ListTagsForResource',
          'ssm:PutParameter',
          's3:ListAllMyBuckets',
          'tag:GetResources',
        ],
        resources: ['*'],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['s3:GetObject', 's3:ListBucket'],
        resources: [binaryBucketArn, `${binaryBucketArn}/*`],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: [
          's3:CreateBucket',
          's3:DeleteBucket',
          's3:PutBucketTagging',
          's3:GetBucketTagging',
          's3:ListBucket',
          's3:PutObject*',
          's3:DeleteObject',
          's3:ListAllMyBuckets',
        ],
        resources: [`arn:aws:s3:::${podIdentityS3BucketPrefix}*`],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['eks:CreateAccessEntry', 'eks:DescribeCluster', 'eks:ListClusters', 'eks:TagResource'],
        resources: [
          `arn:aws:eks:${stack.region}:${stack.account}:cluster/*`,
          `arn:aws:eks:${stack.region}:${stack.account}:access-entry/*`,
        ],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['eks:CreateCluster'],
        resources: [`arn:aws:eks:${stack.region}:${stack.account}:cluster/*`],
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['eks:DeleteCluster', 'eks:ListUpdates', 'eks:DescribeUpdate'],
        resources: [`arn:aws:eks:${stack.region}:${stack.account}:cluster/*`],
        conditions: resourceTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['eks:DeleteAccessEntry', 'eks:DescribeAccessEntry', 'eks:ListAssociatedAccessPolicies'],
        resources: [`arn:aws:eks:${stack.region}:${stack.account}:access-entry/*`],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['eks:CreateAddon', 'eks:CreatePodIdentityAssociation'],
        resources: [`arn:aws:eks:${stack.region}:${stack.account}:cluster/*`],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['eks:DeleteAddon', 'eks:DescribeAddon'],
        resources: [`arn:aws:eks:${stack.region}:${stack.account}:addon/*`],
      }),
      new iam.PolicyStatement({
        actions: ['ssm:SendCommand'],
        resources: [
          'arn:aws:ec2:*:*:instance/*',
          'arn:aws:ssm:*:*:managed-instance/*',
          'arn:aws:ssm:*::document/AWS-RunShellScript',
        ],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: ['ssm:GetCommandInvocation'],
        resources: [`arn:aws:ssm:*:${stack.account}:*`],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: ['ssm:CreateActivation', 'ssm:AddTagsToResource'],
        resources: ['*'],
        effect: iam.Effect.ALLOW,
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        actions: ['ssm:DeregisterManagedInstance'],
        resources: [`arn:aws:ssm:${stack.region}:${stack.account}:managed-instance/*`],
        effect: iam.Effect.ALLOW,
        conditions: resourceTagCondition,
      }),
      new iam.PolicyStatement({
        actions: ['ssm:GetParameter'],
        resources: [
          `arn:aws:ssm:${stack.region}:${stack.account}:parameter/*`,
          `arn:aws:ssm:${stack.region}::parameter/*`,
        ],
        effect: iam.Effect.ALLOW,
      }),
      new iam.PolicyStatement({
        actions: ['secretsmanager:GetSecretValue'],
        resources: [`arn:aws:secretsmanager:${stack.region}:${stack.account}:secret:*`],
      }),
    ],
  });


  const additionalPolicy = new iam.ManagedPolicy(stack, 'nodeadm-e2e-tests-additional-policy', {
    statements: [
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: [
          'cloudformation:DescribeStackEvents',
          'cloudformation:DescribeStacks',
          'cloudformation:DescribeStackResource',
          'cloudformation:UpdateStack',
          'cloudformation:CreateChangeSet',
          'cloudformation:ExecuteChangeSet',
          'cloudformation:DescribeChangeSet',
        ],
        resources: [`arn:aws:cloudformation:${stack.region}:${stack.account}:stack/*`],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['cloudformation:CreateStack'],
        resources: [`arn:aws:cloudformation:${stack.region}:${stack.account}:stack/*`],
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['cloudformation:DeleteStack'],
        resources: [`arn:aws:cloudformation:${stack.region}:${stack.account}:stack/*`],
        conditions: resourceTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['rolesanywhere:CreateTrustAnchor', 'rolesanywhere:CreateProfile'],
        resources: ['*'],
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['rolesanywhere:ListTagsForResource'],
        resources: [
          `arn:aws:rolesanywhere:${stack.region}:${stack.account}:trust-anchor/*`,
          `arn:aws:rolesanywhere:${stack.region}:${stack.account}:profile/*`,
        ],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: [
          'rolesanywhere:DeleteProfile',
          'rolesanywhere:DeleteTrustAnchor',
          'rolesanywhere:GetTrustAnchor',
          'rolesanywhere:GetProfile',
        ],
        resources: [
          `arn:aws:rolesanywhere:${stack.region}:${stack.account}:trust-anchor/*`,
          `arn:aws:rolesanywhere:${stack.region}:${stack.account}:profile/*`,
        ],
        conditions: resourceTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['rolesanywhere:TagResource'],
        resources: [
          `arn:aws:rolesanywhere:${stack.region}:${stack.account}:trust-anchor/*`,
          `arn:aws:rolesanywhere:${stack.region}:${stack.account}:profile/*`,
        ],
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['logs:TagResource'],
        resources: [`arn:aws:logs:${stack.region}:${stack.account}:log-group:/aws/eks/*`],
        conditions: requestTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['logs:PutRetentionPolicy'],
        resources: [`arn:aws:logs:${stack.region}:${stack.account}:log-group:/aws/eks/*`],
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['logs:DeleteLogGroup'],
        resources: [`arn:aws:logs:${stack.region}:${stack.account}:log-group:/aws/eks/*`],
        conditions: resourceTagCondition,
      }),
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['acm-pca:*'],
        resources: [`arn:aws:acm-pca:${stack.region}:${stack.account}:certificate-authority/*`],
      }),
    ],
  });

  return [iamPolicy, ec2Policy, servicesPolicy, additionalPolicy];
}
