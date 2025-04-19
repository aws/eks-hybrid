import * as codepipeline from 'aws-cdk-lib/aws-codepipeline';
import * as codepipeline_actions from 'aws-cdk-lib/aws-codepipeline-actions';
import * as cdk from 'aws-cdk-lib';
import * as codebuild from 'aws-cdk-lib/aws-codebuild';
export function createTestAction(
  kubeVersion: string,
  cni: string,
  buildOutput: codepipeline.Artifact,
  integrationTestProject: codebuild.PipelineProject,
  additionalEnvironmentVariables: { [name: string]: codebuild.BuildEnvironmentVariable } = {},
): codepipeline_actions.CodeBuildAction {
  return new codepipeline_actions.CodeBuildAction({
    actionName: `kube-${kubeVersion.replace('.', '-')}-${cni}`,
    input: buildOutput,
    project: integrationTestProject,
    environmentVariables: {
      KUBERNETES_VERSION: {
        value: kubeVersion,
      },
      CNI: {
        value: 'cilium',
      },
      SKIP_IRA_TEST: {
        value: 'false',
      },
      ...additionalEnvironmentVariables,
    },
  });
}

export function createNodeadmE2EPipeline(
  stack: cdk.Stack,
  nameSuffix: string,
  sourceAction: codepipeline_actions.GitHubSourceAction,
  buildAction: codepipeline_actions.CodeBuildAction,
  cleanupAction: codepipeline_actions.CodeBuildAction,
  ecrCacheAction: codepipeline_actions.CodeBuildAction,
  testsActions: Array<codepipeline_actions.CodeBuildAction>,
  variables: Array<codepipeline.Variable> = [],
  additionalStages: Array<codepipeline.StageProps> = [],
) {
  const pipelineName = `nodeadm-${nameSuffix}`;

  return new codepipeline.Pipeline(stack, `${pipelineName}-pipeline`, {
    pipelineName: pipelineName,
    pipelineType: codepipeline.PipelineType.V2,
    restartExecutionOnUpdate: false,
    variables: variables,
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
        actions: [...testsActions],
      },
      ...additionalStages,
    ],
  });
}
