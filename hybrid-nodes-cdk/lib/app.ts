#!/usr/bin/env node
import 'source-map-support/register';
import * as cdk from 'aws-cdk-lib';
import * as fs from 'fs';
import { NodeadmBuildStack } from './nodeadm-stack';
import * as readline from 'readline';
import { CniEcrStack } from './cni-ecr';

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

  new NodeadmBuildStack(app, 'HybridNodesCdkStack', {
    env: {
      account: devStackConfig.account_id,
      region: devStackConfig.region
    }
  });

  new CniEcrStack(app, 'HybridNodesEcrStack', {
    env: {
      account: devStackConfig.account_id,
      region: devStackConfig.region
    }
  });
} else {
  throw new Error(
    `'cdk_dev_env.json' file is missing. Please run 'gen-cdk-env' script to generate it`
  );
}
