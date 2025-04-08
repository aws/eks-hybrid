import codebuild = require('aws-cdk-lib/aws-codebuild');
import cdk = require('aws-cdk-lib');
import { Duration } from 'aws-cdk-lib';
import secretsmanager = require('aws-cdk-lib/aws-secretsmanager');
import codepipeline = require('aws-cdk-lib/aws-codepipeline');
import s3 = require('aws-cdk-lib/aws-s3');
import iam = require('aws-cdk-lib/aws-iam');
import codepipeline_actions = require('aws-cdk-lib/aws-codepipeline-actions');
import * as fs from 'fs';
import * as constants from './constants';

export class NodeadmBuildStack extends cdk.Stack {
  constructor(scope: cdk.App, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    const devStackConfig = JSON.parse(
      fs.readFileSync('cdk_dev_env.json', 'utf-8')
    );

    const testClusterTagKey = "Nodeadm-E2E-Tests-Cluster"
    const testClusterPrefix = "nodeadm-e2e-tests"
    const podIdentityS3BucketPrefix = "podid"
    const requestTagCondition = {
      StringLike: {
        [`aws:RequestTag/${testClusterTagKey}`]: `${testClusterPrefix}-*`
      } 
    }
    const resourceTagCondition = {
      StringLike: {
        [`aws:ResourceTag/${testClusterTagKey}`]: `${testClusterPrefix}-*`
      } 
    }
    for (const envVar of constants.requiredEnvVars) {
      if (process.env[envVar] === undefined) {
        throw new Error(`Required environment variable '${envVar}' not set`);
      }
    }

    let goproxy = 'direct';
    if (process.env['GOPROXY'] !== undefined && process.env['GOPROXY'] !== '') {
      goproxy = process.env['GOPROXY']!
    } else {
      console.warn(`GOPROXY env var not set or is empty. Defaulting to '${goproxy}'`);
    }

    const githubTokenSecret = new secretsmanager.Secret(this, 'NodeadmE2ETestsGitHubToken', {
      secretName: 'nodeadm-e2e-tests-github-token',
      description: 'Personal Access Token for authenticating to GitHub',
      secretObjectValue: {
        'github-token': cdk.SecretValue.unsafePlainText(process.env.HYBRID_GITHUB_TOKEN!),
      }
    });

    const goproxySecret = new secretsmanager.Secret(this, 'NodeadmE2ETestsGoproxy', {
      secretName: 'nodeadm-e2e-tests-goproxy',
      description: 'Go module proxy endpoint or mode',
      secretObjectValue: {
        endpoint: cdk.SecretValue.unsafePlainText(goproxy),
      }
    });

    let rhelUsername = '';
    let rhelPassword = '';
    if (process.env['RHEL_USERNAME'] !== undefined && process.env['RHEL_USERNAME'] !== '') {
      rhelUsername = process.env['RHEL_USERNAME']!
    } else {
      console.warn(`'RHEL_USERNAME' env var not set or is empty. This will cause Red Hat credentials secret creation to get skipped, which could cause RHEL tests to fail'`);
    }
    if (process.env['RHEL_PASSWORD'] !== undefined && process.env['RHEL_PASSWORD'] !== '') {
      rhelPassword = process.env['RHEL_PASSWORD']!
    } else {
      console.warn(`'RHEL_PASSWORD' env var not set or is empty. This will cause Red Hat credentials secret creation to get skipped, which could cause RHEL tests to fail'`);
    }

    if (rhelUsername !== '' && rhelPassword !== '') {
      const redhatCredentialsSecret = new secretsmanager.Secret(this, 'NodeadmE2ERedHatCredentials', {
        secretName: 'nodeadm-e2e-tests-redhat-credentials',
        description: 'Username and password for authenticating with Red Hat Subscription Manager ',
        secretObjectValue: {
          'username': cdk.SecretValue.unsafePlainText(rhelUsername),
          'password': cdk.SecretValue.unsafePlainText(rhelPassword),
        },
      });
    } else {
      console.warn(`Red Hat credentials secret creation has been skipped due to empty username and/or password environment variables'`);
    }

    const nodeadmBinaryBucket = new s3.Bucket(this, `nodeadm-binaries-${this.account}`, {
      bucketName: `nodeadm-binaries-${this.account}`,
      enforceSSL: true,
      versioned: true,
      encryption: s3.BucketEncryption.S3_MANAGED,
    });

    const nodeadmLogsBucket = new s3.Bucket(this, `nodeadm-logs-${this.account}`, {
      bucketName: `nodeadm-logs-${this.account}`,
      enforceSSL: true,
      versioned: true,
      encryption: s3.BucketEncryption.S3_MANAGED,
    });

    nodeadmLogsBucket.addLifecycleRule({
      enabled: true,
      expiration: Duration.days(30),
    });

    const sourceOutput = new codepipeline.Artifact();
    const sourceAction = new codepipeline_actions.GitHubSourceAction({
      actionName: 'GitHubSource',
      owner: devStackConfig.github_username,
      repo: constants.githubRepo,
      branch: constants.githubBranch,
      oauthToken: githubTokenSecret.secretValueFromJson('github-token'),
      output: sourceOutput,
      trigger: codepipeline_actions.GitHubTrigger.NONE,
    });

    const codeBuildProject = new codebuild.PipelineProject(this, 'nodeadm-build', {
      projectName: 'nodeadm-build',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/build-nodeadm.yml'),
      environmentVariables: {
        GOPROXY: {
          type: codebuild.BuildEnvironmentVariableType.SECRETS_MANAGER,
          value: `${goproxySecret.secretArn}:endpoint`,
        },
        ARTIFACTS_BUCKET: {
          type: codebuild.BuildEnvironmentVariableType.PLAINTEXT,
          value: nodeadmBinaryBucket.bucketName,
        },
        MANIFEST_HOST: {
          type: codebuild.BuildEnvironmentVariableType.PLAINTEXT,
          value: constants.eksReleaseManifestHost,
        },
      },
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
        computeType: codebuild.ComputeType.LARGE,
      },
    });

    codeBuildProject.role!.addToPrincipalPolicy(
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['s3:PutObject*', 's3:ListBucket'],
        resources: [nodeadmBinaryBucket.bucketArn, `${nodeadmBinaryBucket.bucketArn}/*`],
      }),
    );

    const buildOutput = new codepipeline.Artifact();
    const buildAction = new codepipeline_actions.CodeBuildAction({
      actionName: 'Build',
      input: sourceOutput,
      outputs: [buildOutput],
      project: codeBuildProject,
    });

    const testsECRCacheProject = new codebuild.PipelineProject(this, 'ecr-cache', {
      projectName: 'ecr-cache',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/ecr-cache.yml'),
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
        computeType: codebuild.ComputeType.SMALL,
      },
    });

    testsECRCacheProject.role!.addManagedPolicy(iam.ManagedPolicy.fromAwsManagedPolicyName('AmazonEC2ContainerRegistryPullOnly'));

    const ecrCacheAction = new codepipeline_actions.CodeBuildAction({
      actionName: 'ECR-Cache',
      input: buildOutput,
      project: testsECRCacheProject,
    });

    const testsCleanupProject = new codebuild.PipelineProject(this, 'nodeadm-cleanup', {
      projectName: 'nodeadm-cleanup',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/cleanup-nodeadm.yml'),
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
        computeType: codebuild.ComputeType.SMALL,
      },
    });

    const cleanupAction = new codepipeline_actions.CodeBuildAction({
      actionName: 'Cleanup',
      input: buildOutput,
      project: testsCleanupProject,
    });

    const integrationTestProject = new codebuild.PipelineProject(this, 'nodeadm-e2e-tests-project', {
      projectName: 'nodeadm-e2e-tests',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/test-nodeadm.yml'),
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
        environmentVariables: {
          AWS_REGION: {
            value: this.region,
          },
          ARTIFACTS_BUCKET: {
            value: nodeadmBinaryBucket.bucketName,
          },
          LOGS_BUCKET: {
            value: nodeadmLogsBucket.bucketName,
          },
          GOPROXY: {
            type: codebuild.BuildEnvironmentVariableType.SECRETS_MANAGER,
            value: `${goproxySecret.secretArn}:endpoint`,
          },
          CLUSTER_VPC_CIDR: {
            value: constants.clusterVpcCidr,
          },
          CLUSTER_PUBLIC_SUBNET_CIDR: {
            value: constants.clusterPublicSubnetCidr,
          },
          CLUSTER_PRIVATE_SUBNET_CIDR: {
            value: constants.clusterPrivateSubnetCidr,
          },
          HYBRID_VPC_CIDR: {
            value: constants.hybridVpcCidr,
          },
          HYBRID_PUBLIC_SUBNET_CIDR: {
            value: constants.hybridPublicSubnetCidr,
          },
          HYBRID_PRIVATE_SUBNET_CIDR: {
            value: constants.hybridPrivateSubnetCidr,
          },
          HYBRID_POD_CIDR: {
            value: constants.hybridPodCidr,
          },
        },
      },
    });
    integrationTestProject.role!.addToPrincipalPolicy(
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['s3:PutObject*'],
        resources: [nodeadmLogsBucket.bucketArn, `${nodeadmLogsBucket.bucketArn}/*`],
      }),
    );
    const testCreationCleanupPolicy = new iam.Policy(this, 'nodeadm-e2e-tests-runner-policy', {
      statements: [
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
          resources: [`arn:aws:iam::${this.account}:role/*`],
          effect: iam.Effect.ALLOW,
        }),
        new iam.PolicyStatement({
          actions: [
            'iam:DeleteRolePolicy',
            'iam:ListAttachedRolePolicies',
            'iam:ListInstanceProfilesForRole',
            'iam:ListRolePolicies',  
          ],
          resources: [`arn:aws:iam::${this.account}:role/*`],
          effect: iam.Effect.ALLOW,
          conditions: resourceTagCondition,
        }),
        new iam.PolicyStatement({
          actions: [
            'iam:CreateRole',
          ],
          resources: [`arn:aws:iam::${this.account}:role/*`],
          effect: iam.Effect.ALLOW,
          conditions: requestTagCondition,
        }),
        new iam.PolicyStatement({
          actions: [
            'iam:DeleteRole',
          ],
          resources: [`arn:aws:iam::${this.account}:role/*`],
          effect: iam.Effect.ALLOW,
          conditions: resourceTagCondition,
        }),
        new iam.PolicyStatement({
          actions: [
            'iam:AddRoleToInstanceProfile', // remove after we have cleaned up those without tags in existing accounts
            'iam:CreateInstanceProfile',
            'iam:DeleteInstanceProfile', // remove after we have cleaned up those without tags in existing accounts
            'iam:GetInstanceProfile',
            'iam:ListInstanceProfiles',
            'iam:RemoveRoleFromInstanceProfile',  // remove after we have cleaned up those without tags in existing accounts
          ],
          resources: [`arn:aws:iam::${this.account}:instance-profile/*`],
          effect: iam.Effect.ALLOW,
        }),
        new iam.PolicyStatement({
          actions: [
            'iam:TagInstanceProfile'
          ],
          resources: [`arn:aws:iam::${this.account}:instance-profile/*`],
          effect: iam.Effect.ALLOW,
          conditions: requestTagCondition,
        }),
        new iam.PolicyStatement({
          actions: [
            'iam:AddRoleToInstanceProfile',
            'iam:DeleteInstanceProfile',
            'iam:RemoveRoleFromInstanceProfile',
          ],
          resources: [`arn:aws:iam::${this.account}:instance-profile/*`],
          effect: iam.Effect.ALLOW,
          conditions: resourceTagCondition,
        }),
        new iam.PolicyStatement({
          actions: [
            'ec2:AcceptVpcPeeringConnection',
            'ec2:AssociateRouteTable',
            'ec2:AssociateTransitGatewayRouteTable',
            'ec2:AttachInternetGateway',
            'ec2:AuthorizeSecurityGroupIngress',
            'ec2:CreateVpcPeeringConnection',  
            'ec2:CreateRoute',
            'ec2:CreateRouteTable',
            'ec2:CreateSubnet',
            'ec2:CreateTransitGateway',
            'ec2:CreateTransitGatewayRoute',
            'ec2:CreateTransitGatewayRouteTable',
            'ec2:CreateTransitGatewayVpcAttachment',
            'ec2:DeleteKeyPair',
            'ec2:DeleteNetworkInterface',
            'ec2:DeleteRouteTable',
            'ec2:DeleteSecurityGroup',
            'ec2:DeleteTransitGateway',
            'ec2:DeleteTransitGatewayRoute',
            'ec2:DeleteTransitGatewayRouteTable',
            'ec2:DeleteTransitGatewayVpcAttachment',
            'ec2:DescribeAvailabilityZones',
            'ec2:DescribeImages',
            'ec2:DescribeInstances',
            'ec2:DescribeInstanceStatus',
            'ec2:DescribeInternetGateways',
            'ec2:DescribeKeyPairs',
            'ec2:DescribeNetworkInterfaces',
            'ec2:DescribeRouteTables',
            'ec2:DescribeSecurityGroups',
            'ec2:DescribeSubnets',
            'ec2:DescribeTransitGateways',
            'ec2:DescribeTransitGatewayAttachments',
            'ec2:DescribeTransitGatewayRouteTables',
            'ec2:DescribeTransitGatewayVpcAttachments',
            'ec2:DescribeVpcPeeringConnections',
            'ec2:DescribeVpcs',
            'ec2:DisassociateTransitGatewayRouteTable',
            'ec2:GetTransitGatewayRouteTableAssociations',
            'ec2:ModifyInstanceAttribute',
            'ec2:ModifyVpcAttribute',
            'ec2:ModifySubnetAttribute',
            'ec2:RevokeSecurityGroupIngress',
            'ec2:RunInstances',
            'ec2:SearchTransitGatewayRoutes',
          ],
          resources: ['*'],
          effect: iam.Effect.ALLOW,
        }),
        new iam.PolicyStatement({
          actions: [
            'ec2:CreateInternetGateway',
            'ec2:CreateTags',
            'ec2:CreateVpc',   
            'ec2:CreateKeyPair',
          ],
          resources: ['*'],
          effect: iam.Effect.ALLOW,
          conditions: requestTagCondition,
        }),
        new iam.PolicyStatement({
          actions: [
            'ec2:DeleteInternetGateway',
            'ec2:DeleteRoute',
            'ec2:DeleteSubnet',
            'ec2:DeleteVpc',
            'ec2:DeleteVpcPeeringConnection',
            'ec2:DisassociateRouteTable',
            'ec2:DetachInternetGateway',
            'ec2:RebootInstances',
            'ec2:StopInstances',
            'ec2:TerminateInstances',
            'ec2-instance-connect:SendSerialConsoleSSHPublicKey',
          ],
          resources: ['*'],
          effect: iam.Effect.ALLOW,
          conditions: resourceTagCondition,
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
          resources: [`arn:aws:ssm:*:${this.account}:*`],
          effect: iam.Effect.ALLOW,
        }),
        new iam.PolicyStatement({
          actions: [
            'ssm:DeleteParameter',
            'ssm:DescribeActivations',
            'ssm:DescribeInstanceInformation',
            'ssm:DescribeParameters',
            'ssm:GetParameters',
            'ssm:ListTagsForResource',
            'ssm:PutParameter',
          ],
          resources: ['*'],
          effect: iam.Effect.ALLOW,
        }),
        new iam.PolicyStatement({
          actions: [
            'ssm:CreateActivation',
            'ssm:AddTagsToResource'
          ],
          resources: ['*'],
          effect: iam.Effect.ALLOW,
          conditions: requestTagCondition
        }),
        new iam.PolicyStatement({
          actions: [
            'ssm:DeleteActivation'
          ],
          resources: ['*'],
          effect: iam.Effect.ALLOW
        }),
        new iam.PolicyStatement({
          actions: [
            'ssm:DeregisterManagedInstance'
          ],
          resources: [`arn:aws:ssm:${this.region}:${this.account}:managed-instance/*`],
          effect: iam.Effect.ALLOW,
          conditions: resourceTagCondition
        }),
        new iam.PolicyStatement({
          actions: ['ssm:GetParameter'],
          resources: [
            `arn:aws:ssm:${this.region}:${this.account}:parameter/*`,
            `arn:aws:ssm:${this.region}::parameter/*`,
          ],
          effect: iam.Effect.ALLOW,
        }),
        new iam.PolicyStatement({
          actions: ['secretsmanager:GetSecretValue'],
          resources: [`arn:aws:secretsmanager:${this.region}:${this.account}:secret:*`],
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: ['s3:GetObject', 's3:ListBucket'],
          resources: [constants.eksHybridBetaBucketARN, `${constants.eksHybridBetaBucketARN}/*`, nodeadmBinaryBucket.bucketArn, `${nodeadmBinaryBucket.bucketArn}/*`],
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
          ],
          resources: [`arn:aws:s3:::${podIdentityS3BucketPrefix}*`]
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            's3:ListAllMyBuckets',
          ],
          resources: ['*']
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            'eks:CreateAccessEntry',
            'eks:DescribeCluster',
            'eks:ListClusters',
            'eks:TagResource',
          ],
          resources: [
            `arn:aws:eks:${this.region}:${this.account}:cluster/*`,
            `arn:aws:eks:${this.region}:${this.account}:access-entry/*`
          ],
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            'eks:CreateCluster',
          ],
          resources: [
            `arn:aws:eks:${this.region}:${this.account}:cluster/*`,
          ],
          conditions: requestTagCondition,
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            'eks:DeleteCluster',
          ],
          resources: [
            `arn:aws:eks:${this.region}:${this.account}:cluster/*`,
          ],
          conditions: resourceTagCondition,
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: ['eks:DeleteAccessEntry', 'eks:DescribeAccessEntry', 'eks:ListAssociatedAccessPolicies'],
          resources: [`arn:aws:eks:${this.region}:${this.account}:access-entry/*`],
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: ['eks:CreateAddon', 'eks:CreatePodIdentityAssociation'],
          resources: [`arn:aws:eks:${this.region}:${this.account}:cluster/*`]
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: ['eks:DeleteAddon', 'eks:DescribeAddon'],
          resources: [`arn:aws:eks:${this.region}:${this.account}:addon/*`],
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            'cloudformation:DescribeStackEvents',
            'cloudformation:DescribeStacks',
            'cloudformation:DescribeStackResource',
            'cloudformation:UpdateStack',
          ],
          resources: [`arn:aws:cloudformation:${this.region}:${this.account}:stack/*`],
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            'cloudformation:CreateStack',
          ],
          resources: [`arn:aws:cloudformation:${this.region}:${this.account}:stack/*`],
          conditions: requestTagCondition,
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            'cloudformation:DeleteStack',
          ],
          resources: [`arn:aws:cloudformation:${this.region}:${this.account}:stack/*`],
          conditions: resourceTagCondition,
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            'cloudformation:ListStacks',
            'cloudformation:DescribeStacks',
          ],
          resources: ['*'],
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            "rolesanywhere:CreateTrustAnchor",
            "rolesanywhere:CreateProfile",
          ],
          resources: ['*'],
          conditions: requestTagCondition,
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            "rolesanywhere:TagResource",
          ],
          resources: [
            `arn:aws:rolesanywhere:${this.region}:${this.account}:trust-anchor/*`,
            `arn:aws:rolesanywhere:${this.region}:${this.account}:profile/*`
          ],
          conditions: requestTagCondition,
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            "rolesanywhere:ListTagsForResource"
          ],
          resources: [
            `arn:aws:rolesanywhere:${this.region}:${this.account}:trust-anchor/*`,
            `arn:aws:rolesanywhere:${this.region}:${this.account}:profile/*`
          ],
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            "rolesanywhere:ListTrustAnchors",
            "rolesanywhere:ListProfiles"
          ],
          resources: ['*']
        }),
        new iam.PolicyStatement({
          effect: iam.Effect.ALLOW,
          actions: [
            "rolesanywhere:DeleteProfile",
            "rolesanywhere:DeleteTrustAnchor",
            "rolesanywhere:GetTrustAnchor",
            "rolesanywhere:GetProfile"
          ],
          resources: [
            `arn:aws:rolesanywhere:${this.region}:${this.account}:trust-anchor/*`,
            `arn:aws:rolesanywhere:${this.region}:${this.account}:profile/*`
          ],
          conditions: resourceTagCondition,
        }),
      ],
    });
    integrationTestProject.role!.attachInlinePolicy(testCreationCleanupPolicy);
    testsCleanupProject.role!.attachInlinePolicy(testCreationCleanupPolicy);

    const e2eTestsActions: Array<codepipeline_actions.CodeBuildAction> = [];
    for (const kubeVersion of constants.kubernetesVersions) {
      for (const cni of constants.cnis) {
        const actionName = `kube-${kubeVersion.replace('.', '-')}-${cni}`;

        const e2eTestsAction = new codepipeline_actions.CodeBuildAction({
          actionName: actionName,
          input: buildOutput,
          project: integrationTestProject,
          environmentVariables: {
            KUBERNETES_VERSION: {
              value: kubeVersion,
            },
            CNI: {
              value: cni,
            },
            ...(constants.betaKubeVersions.includes(kubeVersion)
              ? {
                  ENDPOINT: {
                    value: constants.betaEksEndpoint,
                  },
                }
              : {}),
          },
        });
        e2eTestsActions.push(e2eTestsAction);
      }
    }

    const codeBuildReleaseProject = new codebuild.PipelineProject(this, 'nodeadm-upload-artifacts', {
      projectName: 'nodeadm-upload-artifacts',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/dev-release-nodeadm.yml'),
      environmentVariables: {
        ARTIFACTS_BUCKET: {
          type: codebuild.BuildEnvironmentVariableType.PLAINTEXT,
          value: nodeadmBinaryBucket.bucketName,
        },
      },
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
      },
    });
    codeBuildReleaseProject.role!.addToPrincipalPolicy(
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['s3:PutObject*', 's3:ListBucket'],
        resources: [nodeadmBinaryBucket.bucketArn, `${nodeadmBinaryBucket.bucketArn}/*`],
      }),
    );
    const devReleaseAction = new codepipeline_actions.CodeBuildAction({
      actionName: 'Upload-Artifacts',
      input: buildOutput,
      project: codeBuildReleaseProject,
    });

    // Create the CodePipeline with the private GitHub source
    const pipeline = new codepipeline.Pipeline(this, 'nodeadm-e2e-tests-pipeline', {
      pipelineName: 'nodeadm-e2e-tests',
      pipelineType: codepipeline.PipelineType.V2,
      restartExecutionOnUpdate: false,
      stages: [
        {
          stageName: 'Source',
          actions: [sourceAction],
        },
        {
          stageName: 'Build',
          actions: [buildAction],
        },
        {
          stageName: 'CleanupAndCache',
          actions: [cleanupAction, ecrCacheAction],          
        },
        {
          stageName: 'E2E-Tests',
          actions: [...e2eTestsActions],
        },
        {
          stageName: 'Upload-Artifacts',
          actions: [devReleaseAction],
        },
      ],
    });
  }
}
