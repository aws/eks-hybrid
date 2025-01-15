import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import { Construct } from 'constructs';

interface HybridNodesRemoteStackProps extends cdk.StackProps {
  clusterStackVPC: ec2.Vpc;
  awsStackParams: any;
}

export class HybridNodesRemoteStack extends cdk.Stack {

  public readonly vpc: ec2.Vpc;
  public readonly ec2Instance: ec2.Instance;

  constructor(scope: Construct, id: string, props: HybridNodesRemoteStackProps) {
    super(scope, id, props);

    const clusterVPC = props.clusterStackVPC

    // Get the key pair name from an environment variable
    const keyPairName = process.env.HYBRID_REMOTE_NODE_KEY_PAIR || 'default-key-pair';

    // Create an EC2 Key Pair resource using the name from the environment variable
    const keyPair = ec2.KeyPair.fromKeyPairName(this, 'KeyPair', keyPairName);

    // Remote Node VPC immitating onprem machine
    this.vpc = new ec2.Vpc(this, 'HybridNodesRemoteVPC', {
      availabilityZones: ['us-west-2a'],
      enableDnsHostnames: true,
      enableDnsSupport: true,
      ipAddresses: ec2.IpAddresses.cidr('172.17.0.0/16'),
      createInternetGateway: true,
      subnetConfiguration: [
        {
          name: 'Remote-Node-Public-Subnet',
          subnetType: ec2.SubnetType.PUBLIC,
          cidrMask: 24,
        },
        {
          name: 'Remote-Node-Private-Subnet',
          subnetType: ec2.SubnetType.PRIVATE_ISOLATED,
          cidrMask: 24,
        }
      ]
    });

    // EKS Security Group
    const remoteNodeSG = new ec2.SecurityGroup(this, 'EKSRemoteNodeSG', {
      vpc: this.vpc,
      description: 'EKS Remote Node Security Group',
      allowAllOutbound: true,
    });

    remoteNodeSG.connections.allowFrom(
      ec2.Peer.ipv4(clusterVPC.vpcCidrBlock),
      ec2.Port.allTraffic(),
    )
    remoteNodeSG.addIngressRule(ec2.Peer.anyIpv4(), ec2.Port.tcp(22), 'Allow SSH from anywhere')

    const instance1 = new ec2.Instance(this, 'HybridNode01', {
      vpc: this.vpc,
      instanceType: ec2.InstanceType.of(ec2.InstanceClass.T3, ec2.InstanceSize.MEDIUM),
      machineImage: ec2.MachineImage.lookup({
        name: props.awsStackParams.amiNameInstance1,
        owners: ['self']
      }),
      vpcSubnets: this.vpc.selectSubnets({ subnetType: ec2.SubnetType.PUBLIC }),
      keyPair: keyPair,
      securityGroup: remoteNodeSG,
    });

    const instance2 = new ec2.Instance(this, 'HybridNode02', {
      vpc: this.vpc,
      instanceType: ec2.InstanceType.of(ec2.InstanceClass.T3, ec2.InstanceSize.MEDIUM),
      machineImage: ec2.MachineImage.lookup({
        name: props.awsStackParams.amiNameInstance2,
        owners: ['self']
      }),
      vpcSubnets: this.vpc.selectSubnets({ subnetType: ec2.SubnetType.PUBLIC }),
      keyPair: keyPair,
      securityGroup: remoteNodeSG,
    });
  }
}
