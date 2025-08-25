import codebuild = require('aws-cdk-lib/aws-codebuild');
import cdk = require('aws-cdk-lib');
import secretsmanager = require('aws-cdk-lib/aws-secretsmanager');
import codepipeline = require('aws-cdk-lib/aws-codepipeline');
import s3 = require('aws-cdk-lib/aws-s3');
import iam = require('aws-cdk-lib/aws-iam');
import codepipeline_actions = require('aws-cdk-lib/aws-codepipeline-actions');
import * as fs from 'fs';
import * as constants from './constants';
import { createNodeadmTestsCreationCleanupPolicy } from './nodeadm/policies';
import { createNodeadmE2EPipeline, createTestAction } from './nodeadm/e2e';

export class NodeadmBuildStack extends cdk.Stack {
  devStackConfig: any;
  githubProject: GitHubProject;

  cleanupAction: codepipeline_actions.CodeBuildAction | undefined;
  ecrCacheAction: codepipeline_actions.CodeBuildAction | undefined;
  githubSourceAction: codepipeline_actions.GitHubSourceAction | undefined;
  githubSourceOutput: codepipeline.Artifact | undefined;
  githubTokenSecret: secretsmanager.ISecret | undefined;
  goproxySecret: secretsmanager.Secret | undefined;
  integrationTestProject: codebuild.PipelineProject | undefined;
  nodeadmBinaryBucket: s3.Bucket | undefined;
  nodeadmBuildAction: codepipeline_actions.CodeBuildAction | undefined;
  nodeadmBuildOutput: codepipeline.Artifact | undefined;
  nodeadmLogsBucket: s3.Bucket | undefined;
  nodeadmVersionVariable: codepipeline.Variable | undefined;
  testsCleanupProject: codebuild.PipelineProject | undefined;
  testCreationCleanupPolicies: iam.ManagedPolicy[] | undefined;

  constructor(scope: cdk.App, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    this.devStackConfig = JSON.parse(
      fs.readFileSync('cdk_dev_env.json', 'utf-8')
    );

    for (const envVar of constants.requiredEnvVars) {
      if (process.env[envVar] === undefined) {
        throw new Error(`Required environment variable '${envVar}' not set`);
      }
    }

    this.githubProject = {
      Owner: this.devStackConfig.github_username,
      Repo: constants.githubRepo,
      Branch: constants.githubBranch,
    };

    this.createSecrets();

    this.createBinaryBucket();
    this.createLogsBucket();

    this.createCreationCleanupPolicy();

    this.createSourceAction();
    this.createNodeadmBuild(this.goproxySecret!.secretArn, constants.eksReleaseManifestHost);
    this.createECRCacheBuild();
    this.createCleanupBuild();
    this.createIntegrationTestBuild();

    this.createE2EPipeline();
    this.createConformancePipeline();
    this.createAddonPipeline()
  }

  createSecrets() {
    let goproxy = 'direct';
    if (process.env['GOPROXY'] !== undefined && process.env['GOPROXY'] !== '') {
      goproxy = process.env['GOPROXY']!
    } else {
      console.warn(`GOPROXY env var not set or is empty. Defaulting to '${goproxy}'`);
    }

    this.githubTokenSecret = new secretsmanager.Secret(this, 'NodeadmE2ETestsGitHubToken', {
      secretName: 'nodeadm-e2e-tests-github-token',
      description: 'Personal Access Token for authenticating to GitHub',
      secretObjectValue: {
        'github-token': cdk.SecretValue.unsafePlainText(process.env.HYBRID_GITHUB_TOKEN!),
      }
    });

    this.goproxySecret = new secretsmanager.Secret(this, 'NodeadmE2ETestsGoproxy', {
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
  }

  createBinaryBucket() {
    this.nodeadmBinaryBucket = new s3.Bucket(this, `nodeadm-binaries-${this.account}`, {
      bucketName: `nodeadm-binaries-${this.account}`,
      enforceSSL: true,
      versioned: true,
      encryption: s3.BucketEncryption.S3_MANAGED,
    });
    this.addStandardLifecycleRules(this.nodeadmBinaryBucket);
  }

  createLogsBucket() {
    this.nodeadmLogsBucket = new s3.Bucket(this, `nodeadm-logs-${this.account}`, {
      bucketName: `nodeadm-logs-${this.account}`,
      enforceSSL: true,
      versioned: true,
      encryption: s3.BucketEncryption.S3_MANAGED,
    });
    this.addStandardLifecycleRules(this.nodeadmLogsBucket);
  }

  createCreationCleanupPolicy() {
    if (this.nodeadmBinaryBucket === undefined) {
      throw new Error('Nodeadm binary bucket is not defined');
    }

    this.testCreationCleanupPolicies = createNodeadmTestsCreationCleanupPolicy(
      this,
      constants.testClusterTagKey,
      constants.testClusterPrefix,
      this.nodeadmBinaryBucket.bucketArn,
      constants.podIdentityS3BucketPrefix,
    );
  }

  createGitHubSourceAction(trigger: codepipeline_actions.GitHubTrigger) {
    if (this.githubTokenSecret === undefined) {
      throw new Error('`githubTokenSecret` is not defined');
    }
    if (this.githubSourceOutput === undefined) {
      throw new Error('`githubSourceOutput` is not defined');
    }

    return new codepipeline_actions.GitHubSourceAction({
      actionName: 'GitHubSource',
      owner: this.githubProject.Owner,
      repo: this.githubProject.Repo,
      branch: this.githubProject.Branch,
      oauthToken: this.githubTokenSecret.secretValueFromJson('github-token'),
      output: this.githubSourceOutput,
      trigger: trigger,
    });
  }

  createSourceAction() {
    this.githubSourceOutput = new codepipeline.Artifact();
    this.githubSourceAction = this.createGitHubSourceAction(codepipeline_actions.GitHubTrigger.NONE);
  }

  createNodeadmBuild(goproxySecretArn: string, eksReleaseManifestHost: string) {
    if (this.nodeadmBinaryBucket === undefined) {
      throw new Error('`nodeadmBinaryBucket` is not defined');
    }
    if (this.githubSourceOutput === undefined) {
      throw new Error('`githubSourceOutput` is not defined');
    }
    const codeBuildProject = new codebuild.PipelineProject(this, 'nodeadm-build', {
      projectName: 'nodeadm-build',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/build-nodeadm.yml'),
      environmentVariables: {
        GOPROXY: {
          type: codebuild.BuildEnvironmentVariableType.SECRETS_MANAGER,
          value: `${goproxySecretArn}:endpoint`,
        },
        ARTIFACTS_BUCKET: {
          type: codebuild.BuildEnvironmentVariableType.PLAINTEXT,
          value: this.nodeadmBinaryBucket.bucketName,
        },
        MANIFEST_HOST: {
          type: codebuild.BuildEnvironmentVariableType.PLAINTEXT,
          value: eksReleaseManifestHost,
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
        resources: [this.nodeadmBinaryBucket.bucketArn, `${this.nodeadmBinaryBucket.bucketArn}/*`],
      }),
    );

    this.nodeadmVersionVariable = new codepipeline.Variable({
      variableName: 'nodeadmVersion',
      description: 'semantic version for nodeadm',
      defaultValue: 'v1.0.4-dev',
    });

    this.nodeadmBuildOutput = new codepipeline.Artifact();
    this.nodeadmBuildAction = new codepipeline_actions.CodeBuildAction({
      actionName: 'Build',
      input: this.githubSourceOutput,
      outputs: [this.nodeadmBuildOutput],
      project: codeBuildProject,
      environmentVariables: {
        GIT_VERSION: {
          value: '#{variables.nodeadmVersion}',
        },
      },
    });
  }

  createECRCacheBuild() {
    if (this.nodeadmBuildOutput === undefined) {
      throw new Error('`nodeadmBuildOutput` is not defined');
    }

    const testsECRCacheProject = new codebuild.PipelineProject(this, 'ecr-cache', {
      projectName: 'ecr-cache',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/ecr-cache.yml'),
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
        computeType: codebuild.ComputeType.SMALL,
      },
    });

    testsECRCacheProject.role!.addManagedPolicy(
      iam.ManagedPolicy.fromAwsManagedPolicyName('AmazonEC2ContainerRegistryPullOnly'),
    );

    this.ecrCacheAction = new codepipeline_actions.CodeBuildAction({
      actionName: 'ECR-Cache',
      input: this.nodeadmBuildOutput,
      project: testsECRCacheProject,
    });
  }

  createCleanupBuild() {
    if (this.nodeadmBuildOutput === undefined) {
      throw new Error('`nodeadmBuildOutput` is not defined');
    }
    if (this.testCreationCleanupPolicies === undefined) {
      throw new Error('`testCreationCleanupPolicies` is not defined');
    }

    this.testsCleanupProject = new codebuild.PipelineProject(this, 'nodeadm-cleanup', {
      projectName: 'nodeadm-cleanup',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/cleanup-nodeadm.yml'),
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
        computeType: codebuild.ComputeType.SMALL,
      },
    });

    this.cleanupAction = new codepipeline_actions.CodeBuildAction({
      actionName: 'Cleanup',
      input: this.nodeadmBuildOutput,
      project: this.testsCleanupProject,
    });

    for (const policy of this.testCreationCleanupPolicies) {
      this.testsCleanupProject.role!.addManagedPolicy(policy);
    }
  }

  vpcParams() {
    return {
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
    }
  }

  createIntegrationTestBuild() {
    if (this.nodeadmBinaryBucket === undefined) {
      throw new Error('`nodeadmBinaryBucket` is not defined');
    }
    if (this.goproxySecret === undefined) {
      throw new Error('`goproxySecret` is not defined');
    }
    if (this.nodeadmLogsBucket === undefined) {
      throw new Error('`nodeadmLogsBucket` is not defined');
    }
    if (this.testCreationCleanupPolicies === undefined) {
      throw new Error('`testCreationCleanupPolicies` is not defined');
    }

    this.integrationTestProject = new codebuild.PipelineProject(this, 'nodeadm-e2e-tests-project', {
      projectName: 'nodeadm-e2e-tests',
      buildSpec: codebuild.BuildSpec.fromSourceFilename('buildspecs/test-nodeadm.yml'),
      environment: {
        buildImage: codebuild.LinuxBuildImage.fromDockerRegistry(constants.builderBaseImage),
        environmentVariables: {
          AWS_REGION: {
            value: this.region,
          },
          ARTIFACTS_BUCKET: {
            value: this.nodeadmBinaryBucket.bucketName,
          },
          LOGS_BUCKET: {
            value: this.nodeadmLogsBucket.bucketName,
          },
          GOPROXY: {
            type: codebuild.BuildEnvironmentVariableType.SECRETS_MANAGER,
            value: `${this.goproxySecret.secretArn}:endpoint`,
          },
          ...this.vpcParams(),
        },
      },
    });
    this.integrationTestProject.role!.addToPrincipalPolicy(
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['s3:PutObject*'],
        resources: [this.nodeadmLogsBucket.bucketArn, `${this.nodeadmLogsBucket.bucketArn}/*`],
      }),
    );
    this.integrationTestProject.role!.addToPrincipalPolicy(
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['s3:PutObject*', 's3:ListBucket'],
        resources: [this.nodeadmBinaryBucket.bucketArn, `${this.nodeadmBinaryBucket.bucketArn}/*`],
      }),
    );
    this.integrationTestProject.role!.addToPrincipalPolicy(
      new iam.PolicyStatement({
        effect: iam.Effect.ALLOW,
        actions: ['ecr-public:GetAuthorizationToken', 'sts:GetServiceBearerToken'],
        resources: ['*'],
      }),
    );

    for (const policy of this.testCreationCleanupPolicies) {
      this.integrationTestProject.role!.addManagedPolicy(policy);
    }
  }

  createE2EPipeline() {
    if (this.nodeadmBuildOutput === undefined) {
      throw new Error('`nodeadmBuildOutput` is not defined');
    }
    if (this.nodeadmBuildAction === undefined) {
      throw new Error('`nodeadmBuildAction` is not defined');
    }
    if (this.ecrCacheAction === undefined) {
      throw new Error('`ecrCacheAction` is not defined');
    }
    if (this.cleanupAction === undefined) {
      throw new Error('`cleanupAction` is not defined');
    }
    if (this.githubSourceAction === undefined) {
      throw new Error('`githubSourceAction` is not defined');
    }
    if (this.integrationTestProject === undefined) {
      throw new Error('`integrationTestProject` is not defined');
    }
    if (this.nodeadmVersionVariable === undefined) {
      throw new Error('`nodeadmVersionVariable` is not defined');
    }

    const e2eTestsActions: Array<codepipeline_actions.CodeBuildAction> = [];
    for (const kubeVersion of constants.kubernetesVersions) {
      for (const cni of constants.cnis) {
        let additionalEnvironmentVariables = {};
        if (constants.betaKubeVersions.includes(kubeVersion)) {
          additionalEnvironmentVariables = this.betaEnvironmentVariables();
        }
        const e2eTestsAction = createTestAction(
          kubeVersion,
          cni,
          this.nodeadmBuildOutput,
          this.integrationTestProject,
          additionalEnvironmentVariables,
        );
        e2eTestsActions.push(e2eTestsAction);
      }
    }

    // Create the CodePipeline with the private GitHub source
    const e2ePipeline = createNodeadmE2EPipeline(
      this,
      'e2e-tests',
      this.githubSourceAction,
      this.nodeadmBuildAction,
      this.cleanupAction,
      this.ecrCacheAction,
      e2eTestsActions,
      [this.nodeadmVersionVariable],
    );

    this.addStandardLifecycleRules(e2ePipeline.artifactBucket as s3.Bucket);
  }

  createConformancePipeline() {
    if (this.nodeadmBuildOutput === undefined) {
      throw new Error('`nodeadmBuildOutput` is not defined');
    }
    if (this.nodeadmBuildAction === undefined) {
      throw new Error('`nodeadmBuildAction` is not defined');
    }
    if (this.ecrCacheAction === undefined) {
      throw new Error('`ecrCacheAction` is not defined');
    }
    if (this.cleanupAction === undefined) {
      throw new Error('`cleanupAction` is not defined');
    }
    if (this.githubSourceAction === undefined) {
      throw new Error('`githubSourceAction` is not defined');
    }
    if (this.integrationTestProject === undefined) {
      throw new Error('`integrationTestProject` is not defined');
    }
    if (this.nodeadmVersionVariable === undefined) {
      throw new Error('`nodeadmVersionVariable` is not defined');
    }

    const conformanceActions: Array<codepipeline_actions.CodeBuildAction> = [];
    for (const kubeVersion of constants.kubernetesVersions) {
      const cni = 'cilium';
      let additionalEnvironmentVariables = {
        E2E_SUITE: {
          value: 'conformance.test',
        },
        E2E_FILTER: {
          value: 'conformance',
        },
      };
      if (constants.betaKubeVersions.includes(kubeVersion)) {
        additionalEnvironmentVariables = { ...additionalEnvironmentVariables, ...this.betaEnvironmentVariables() };
      }
      const e2eTestsAction = createTestAction(
        kubeVersion,
        cni,
        this.nodeadmBuildOutput,
        this.integrationTestProject,
        additionalEnvironmentVariables,
      );
      conformanceActions.push(e2eTestsAction);
    }

    const conformancePipeline = createNodeadmE2EPipeline(
      this,
      'conformance',
      this.githubSourceAction,
      this.nodeadmBuildAction,
      this.cleanupAction,
      this.ecrCacheAction,
      conformanceActions,
      [this.nodeadmVersionVariable],
    );

    this.addStandardLifecycleRules(conformancePipeline.artifactBucket as s3.Bucket);
  }

  createAddonPipeline() {
    if (this.nodeadmBuildOutput === undefined) {
      throw new Error('`nodeadmBuildOutput` is not defined');
    }
    if (this.nodeadmBuildAction === undefined) {
      throw new Error('`nodeadmBuildAction` is not defined');
    }
    if (this.ecrCacheAction === undefined) {
      throw new Error('`ecrCacheAction` is not defined');
    }
    if (this.cleanupAction === undefined) {
      throw new Error('`cleanupAction` is not defined');
    }
    if (this.githubSourceAction === undefined) {
      throw new Error('`githubSourceAction` is not defined');
    }
    if (this.integrationTestProject === undefined) {
      throw new Error('`integrationTestProject` is not defined');
    }
    if (this.nodeadmVersionVariable === undefined) {
      throw new Error('`nodeadmVersionVariable` is not defined');
    }

    const addonActions: Array<codepipeline_actions.CodeBuildAction> = [];
    for (const kubeVersion of constants.kubernetesVersions) {
      const cni = 'cilium';
      let additionalEnvironmentVariables = {
        E2E_SUITE: {
          value: 'addons.test',
        },
        E2E_FILTER: {
          value: '',
        },
      };
      if (constants.betaKubeVersions.includes(kubeVersion)) {
        additionalEnvironmentVariables = { ...additionalEnvironmentVariables, ...this.betaEnvironmentVariables() };
      }
      const e2eTestsAction = createTestAction(
        kubeVersion,
        cni,
        this.nodeadmBuildOutput,
        this.integrationTestProject,
        additionalEnvironmentVariables,
      );
      addonActions.push(e2eTestsAction);
    }

    const addonsPipeline = createNodeadmE2EPipeline(
      this,
      'addons',
      this.githubSourceAction,
      this.nodeadmBuildAction,
      this.cleanupAction,
      this.ecrCacheAction,
      addonActions,
      [this.nodeadmVersionVariable],
    );

    this.addStandardLifecycleRules(addonsPipeline.artifactBucket as s3.Bucket);
  }

  addStandardLifecycleRules(bucket: s3.Bucket) {
    bucket.addLifecycleRule({
      enabled: true,
      expiration: cdk.Duration.days(30),
      noncurrentVersionExpiration: cdk.Duration.days(1),
    });
  }

  betaEnvironmentVariables() {
    let betaEksEndpoint = process.env['BETA_EKS_ENDPOINT'] ?? '';
    let betaDefaultClusterRoleSP = process.env['BETA_EKS_CLUSTER_ROLE_SP'] ?? '';
    let betaDefaultPodIdentitySP = process.env['BETA_EKS_POD_IDENTITY_SP'] ?? '';
    if (betaEksEndpoint === '' || betaDefaultClusterRoleSP === '' || betaDefaultPodIdentitySP === '') {
      throw new Error('BETA_EKS_ENDPOINT, BETA_EKS_CLUSTER_ROLE_SP, and BETA_EKS_POD_IDENTITY_SP must be set when isBeta is true');
    }

    return { betaEksEndpoint, betaDefaultClusterRoleSP, betaDefaultPodIdentitySP };
  }
}

export interface GitHubProject {
  readonly Owner: string;
  readonly Repo: string;
  readonly Branch: string;
}