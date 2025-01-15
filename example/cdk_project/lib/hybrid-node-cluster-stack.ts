import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as iam from 'aws-cdk-lib/aws-iam';
import { Construct } from 'constructs';


export class HybridNodesClusterStack extends cdk.Stack {

  public readonly vpc: ec2.Vpc;
  public readonly ec2Instance: ec2.Instance;

  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    // EKS Cluster's VPC
    // Creates subnets per AZ, so this code creates 2 total, two in each provided zone.
    this.vpc = new ec2.Vpc(this, 'HybridNodesClusterVPC', {
      availabilityZones: ['us-west-2a', 'us-west-2b'],
      enableDnsHostnames: true,
      enableDnsSupport: true,
      ipAddresses: ec2.IpAddresses.cidr('10.226.96.0/23'),
      createInternetGateway: true,
      subnetConfiguration: [
        {
          name: 'Hybid Node Public Subnet',
          subnetType: ec2.SubnetType.PUBLIC,
        },
      ]
    });

    // EKS Security Group
    const eksClusterSG = new ec2.SecurityGroup(this, 'EKSClusterSG', {
      vpc: this.vpc,
      description: 'EKS Cluster Security Group',
      allowAllOutbound: true

    });

    // Ingress rules for Pod and Node IP's
    eksClusterSG.addIngressRule(ec2.Peer.ipv4('172.17.0.0/16'), ec2.Port.tcp(443), "Node IP Ingress");
    eksClusterSG.addIngressRule(ec2.Peer.ipv4('172.18.0.0/16'), ec2.Port.tcp(443),  "Pod IP Ingress");

    // EKS IAM role
    const eksClusterRole = new iam.Role(this, 'hybrid-eks-cluster-role', {
      roleName: 'hybrid-eks-cluster-role',
      assumedBy: new iam.ServicePrincipal('eks.amazonaws.com'),
      managedPolicies: [
        iam.ManagedPolicy.fromManagedPolicyArn(this, "eks-cluster-role", "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"),
      ]
    })

    cdk.Tags.of(eksClusterRole).add('name', 'hybrid-eks-role');
    cdk.Tags.of(eksClusterRole).add('app', 'hybrid-eks-beta');
  }
}
