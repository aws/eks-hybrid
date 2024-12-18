import cdk = require('aws-cdk-lib');
import { aws_ecr as ecr, RemovalPolicy } from 'aws-cdk-lib';
import secretsmanager = require('aws-cdk-lib/aws-secretsmanager');
import * as fs from 'fs';

const ciliumRepos = ["cilium/cilium", "cilium/operator-generic"]
const calicoRepos = ["calico/typha", "calico/ctl", "calico/node", "calico/cni", "calico/apiserver", 
    "calico/kube-controllers", "calico/dikastes","calico/pod2daemon-flexvol", "calico/csi", 
    "calico/node-driver-registrar", "calico/cni-windows", "calico/node-windows", "tigera/operator"]

export class CniEcrStack extends cdk.Stack {
    constructor(scope: cdk.App, id: string, props?: cdk.StackProps) {
      super(scope, id, props);
    const devStackConfig = JSON.parse(
      fs.readFileSync('cdk_dev_env.json', 'utf-8')
    );

    var dockerUsername=''
    var dockerToken=''
    if (process.env['HYBRID_DOCKER_USERNAME'] !== undefined && process.env['HYBRID_DOCKER_USERNAME'] !== '') {
        dockerUsername = process.env['HYBRID_DOCKER_USERNAME']!
    } else {
        console.warn(`'HYBRID_DOCKER_USERNAME' env var not set or is empty. ECR pull thru cache setup will be skipped'`);
        return
    }
    if (process.env['HYBRID_DOCKER_TOKEN'] !== undefined && process.env['HYBRID_DOCKER_TOKEN'] !== '') {
        dockerToken = process.env['HYBRID_DOCKER_TOKEN']!
    } else {
        console.warn(`'HYBRID_DOCKER_TOKEN' env var not set or is empty. ECR pull thru cache setup will be skipped'`);
        return
    }
  
    this.createCiliumRepos()
    this.createCalicoRepos()
    this.createPullThroughRules()

    if (this.region === "us-west-2") {
        this.createImageReplication()
    }
  }

  createCiliumRepos() {
    for (const repo of ciliumRepos) {
        new ecr.Repository(this, repo, {
            repositoryName: `quay.io/${repo}`,
            removalPolicy: RemovalPolicy.DESTROY,
            emptyOnDelete: true,
        });
    }
  }

  createCalicoRepos() {
    for (const repo of calicoRepos) {
        var fullRepo = `docker.io/${repo}`
        if (repo.startsWith('tigera')){
            fullRepo = `quay.io/${repo}`
        }
        new ecr.Repository(this, repo, {
            repositoryName: `docker.io/${repo}`,
            removalPolicy: RemovalPolicy.DESTROY,
            emptyOnDelete: true,
        });
    }
  }

  createPullThroughRules(){
    const dockerHubSecret = new secretsmanager.Secret(this, 'NodeadmE2ETestsDockerHubToken', {
        secretName: 'ecr-pullthroughcache/docker-hub',
        description: 'Personal Access Token for authenticating to Docker.io',
        secretObjectValue: {
            'username': cdk.SecretValue.unsafePlainText(process.env['HYBRID_DOCKER_USERNAME']!),
            'accessToken': cdk.SecretValue.unsafePlainText(process.env['HYBRID_DOCKER_TOKEN']!),
        }
    });

    new ecr.CfnPullThroughCacheRule(this, 'dockerhub',{
        ecrRepositoryPrefix: 'docker.io',
        upstreamRegistryUrl: 'registry-1.docker.io',
        upstreamRegistry: 'docker-hub',
        credentialArn: dockerHubSecret.secretArn
    });

    new ecr.CfnPullThroughCacheRule(this, 'quay',{
        ecrRepositoryPrefix: 'quay.io',
        upstreamRegistryUrl: 'quay.io',
        upstreamRegistry: 'quay'
    });
  }

  createImageReplication(){
    new ecr.CfnReplicationConfiguration(this, 'ecr-replication-us-west-1', {
        replicationConfiguration: {
          rules: [{
            destinations: [{
              region: 'us-west-1',
              registryId: this.account,
            }],
      
            // the properties below are optional
            repositoryFilters:  [
                {
                    filter: 'quay.io/tigera',
                    filterType: 'PREFIX_MATCH',
                },
                {
                    filter: 'quay.io/cilium',
                    filterType: 'PREFIX_MATCH',
                },
                {
                    filter: 'docker.io/calico',
                    filterType: 'PREFIX_MATCH',
                },
            ],
          }],
        },
      });
  }
}