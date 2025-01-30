#!/usr/bin/env node
import 'source-map-support/register';
import * as cdk from 'aws-cdk-lib';
import { SecretsManagerClient, DescribeSecretCommand } from "@aws-sdk/client-secrets-manager";
import * as fs from 'fs';
import { NodeadmBuildStack } from './nodeadm-stack';
import * as readline from 'readline';
import { mainModule } from 'process';

const githubSecretName = 'nodeadm-e2e-tests-github-token';

async function main() {
  const app = new cdk.App();

  if (fs.existsSync('cdk_dev_env.json')) {
    const devStackConfig = JSON.parse(
      fs.readFileSync('cdk_dev_env.json', 'utf-8')
    );

    if (!devStackConfig.account_id) {
      throw new Error(
        `'cdk_dev_env.json' is missing required '.account_id' property`
      );
    }

    if (!devStackConfig.region) {
      throw new Error(
        `'cdk_dev_env.json' is missing required '.region' property`
      );
    }

    if (!devStackConfig.github_username) {
      throw new Error(
        `'cdk_dev_env.json' is missing required '.github_username' property`
      );
    }

    const githubSecretExists = await secretExists(new SecretsManagerClient({}), githubSecretName);
    const githubToken = process.env['HYBRID_GITHUB_TOKEN'];
    if (!githubSecretExists && githubToken === undefined) {
      throw new Error(
        `Github secret '${githubSecretName}' does not exist and 'HYBRID_GITHUB_TOKEN' environment variable is not set`
      );
    }
    const reuseGithubSecret = githubSecretExists && githubToken === undefined;

    new NodeadmBuildStack(app, 'HybridNodesCdkStack', {
      env: {
        account: devStackConfig.account_id,
        region: devStackConfig.region,
      },
      githubSecretName: githubSecretName,
      reuseGithubSecret: reuseGithubSecret,
      githubToken: githubToken,
    });
  } else {
    throw new Error(
      `'cdk_dev_env.json' file is missing. Please run 'gen-cdk-env' script to generate it`
    );
  }
}

async function secretExists(client: SecretsManagerClient, name: string): Promise<boolean> {
  const command = new DescribeSecretCommand({ SecretId: name });
  try {
    await client.send(command);
    return true;
  } catch (error: any) {
    if (error.name === "ResourceNotFoundException") {
      return false;
    }
    throw new Error(`Error checking secret existence: ${error.message}`);
  }
}

main();
