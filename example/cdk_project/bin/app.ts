#!/usr/bin/env node
import 'source-map-support/register';
import * as cdk from 'aws-cdk-lib';
import { HybridNodesClusterStack } from '../lib/hybrid-node-cluster-stack';
import { HybridNodesRemoteStack } from '../lib/hybrid-node-remote-stack'
import { PeeringStack } from '../lib/peering-stack';

let awsStackParams: any;

try {
  awsStackParams = require('../parameters.json');
} catch (error) {
  console.error("Error: Ensure that you have a parameters.json file in the root of this directory with your account number and two instances filled in.");
  console.error(error);
  process.exit(1);
}


const app = new cdk.App();

const env = {account: awsStackParams.aws_account, region: awsStackParams.aws_region}

const hybridNodesClusterStack = new HybridNodesClusterStack(app, 'HybridNodesClusterStack', {
  env,
  description: "Hybrid Nodes Cluster Stack holding the EKS Cluster."
 });

 const hybridNodesRemoteStack = new HybridNodesRemoteStack(app, 'HybridNodesRemoteStack', {
  env,
  clusterStackVPC: hybridNodesClusterStack.vpc,
  description: "Hybrid Nodes Remote Stack holding the EC2 Instances simulating Hybrid Nodes.",
  awsStackParams
});

new PeeringStack(app, 'HybridNodesPeeringStack', {
  env,
  clusterVPC: hybridNodesClusterStack.vpc,
  remoteVPC: hybridNodesRemoteStack.vpc,
  description: "Peering connection between Cluster Stack VPC and Remote Node Stack VPC."
});
