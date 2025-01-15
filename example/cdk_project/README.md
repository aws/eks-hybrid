# Hybrid Nodes CDK Demo Usage

This CDK template provides a demo of hybrid nodes utilizing a remote VPC simulating an on-prem node that connects to an EKS cluster. It allows a user to specify two separate AMI's in a configuration file to use as "on-prem" nodes, using nodeadm-preinstalled images created using the hybrid nodes Packer template.

## Prerequisites

Beforing working with this CDK template, make sure you complete the CDK [prerequisites](https://docs.aws.amazon.com/cdk/v2/guide/prerequisites.html).

Use the Node Package Manager to install the CDK CLI. We recommend that you install it globally using the following command:

```
$ npm install -g aws-cdk
```

To install a specific version of the CDK CLI, use the following command structure. This has been tested to work with CDK 2.164.1 and recommend this version to deploy:
```
$ npm install -g aws-cdk@2.164.1
```

Verify the install by running the following:
```
$ cdk --version
```

Fill out the included `parameters.json` file in the root folder of this repo with the below information. Make sure to add it to `.gitignore` when commiting to avoid pushing up your AWS account number:

```
{
    "aws_account":"AWS_ACCOUNT_NUMBER",
    "aws_region":"us-west-2",
    "amiNameInstance1": "AMI_NAME_FOR_INSTANCE_ONE",
    "amiNameInstance2": "AMI_NAME_FOR_INSTANCE_TWO"
}
```

Utilize the official Hybrid Nodes Packer templates to ensure that the AMI's referenced include pre-installed `nodeadm` binaries. The currently supported OS group is:
- RHEL 8/9
- Ubuntu 22.04/24.04

AMI's created with the Packer templates are uploaded to your AWS account's AMI catalog, and `amiNameInstance1`, `amiNameInstance2` expect the AMI Name.

In order to SSH into your AMI instance, you'll need to specify a key pair. For simplicity, the template uses the same key pair and assigns it to both instances. To set your key pair, create the following environment variable specifying the key pair file name:

```
export HYBRID_REMOTE_NODE_KEY_PAIR=<KEY_PAIR_FILE_NAME.pem>
```

Once this is set, you're ready to proceed with creating the separate stacks needed to start.

## Setup Instructions

1. With your `parameters.json` in the root of your copy of this project, ensure that the project is bootstrapped to your account by running the following from within the root directory:

```
$ cdk bootstrap
```

2. Once this environment is properly bootstrapped, run the following to create the three required stacks to your AWS account via CDK, `HybridNodesClusterStack`, `HybridNodesRemoteStack`, and `HybridNodesPeeringStack`:

```
$ cdk deploy --all
```

You can also obtain the CloudFormation templates in JSON format from this project to work with instead, by running the following command (make sure your parameters.json file is set):

```
$ cdk synth
```

3. Once the stacks are deployed to your account, take note of the following:
- The set CIDR block for the two created hybrid nodes is `172.17.0.0/16`
- The assumed POD CIDR block is `172.18.0.0/16`


***Note, the below information will be updated and changed once CDK is updated to include RemoteNetworkConfig after GA ***

## Create EKS Cluster

### AWS CLI

Step 1: Set environment variables for EKS cluster creation
The HYBRID_ACCOUNT_ID must be the AWS account ID that was allowlisted for the hybrid nodes beta.

```
export HYBRID_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export HYBRID_VPC_ID=            # VPC ID to use for EKS cluster
export HYBRID_SUBNET_IDS=        # Comma-delimited list of Subnet IDs
export HYBRID_REMOTE_NODE_CIDRS= # Comma-delimited list of on-premises node CIDRs
export HYBRID_REMOTE_POD_CIDRS=  # Comma-delimited list of on-premises pod CIDRs
export REGION=                   # AWS Region to use for EKS cluster (example: us-west-2)
export HYBRID_SECURITY_IDS=      # Get this from security group created with the Cluster under: VPC > Security Group >
                                 eks-cluster-sg-hybrid-eks-cluster-*


```

Step 2: Create EKS cluster IAM role

```
# Create Role
aws iam create-role \
  --role-name hybrid-eks-cluster-role \
  --assume-role-policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"eks.amazonaws.com"},"Action":"sts:AssumeRole"}]}' \
  --tags Key=Name,Value=hybrid-eks-role Key=App,Value=hybrid-eks-beta

# Attach Role Policy
aws iam attach-role-policy \
  --role-name hybrid-eks-cluster-role \
  --policy-arn arn:aws:iam::aws:policy/AmazonEKSClusterPolicy
```

Step 3: Create hybrid nodes-enabled EKS cluster

```
aws eks create-cluster \
  --name hybrid-eks-cluster \
  --endpoint-url https://eks.${REGION}.amazonaws.com \
  --role-arn arn:aws:iam::${HYBRID_ACCOUNT_ID}:role/hybrid-eks-cluster-role \
  --resources-vpc-config subnetIds=${HYBRID_SUBNET_IDS},securityGroupIds=${HYBRID_SECURITY_IDS} \
  --remote-network-config '{"remoteNodeNetworks":[{"cidrs":["'"${HYBRID_REMOTE_NODE_CIDRS}"'"]}],"remotePodNetworks":[{"cidrs":["'"${HYBRID_REMOTE_POD_CIDRS}"'"]}]}' \
  --access-config authenticationMode=API_AND_CONFIG_MAP \
  --tags Name=hybrid-eks-cluster,App=hybrid-eks-beta
```

To observe the status of your cluster creation, you can view your cluster in the EKS console or you can run the following AWS CLI command. Your cluster has been created successfully when the status field is Active in the output of the describe-cluster command.

```
aws eks describe-cluster \
  --name hybrid-eks-cluster \
  --endpoint-url https://eks.${REGION}.amazonaws.com
```

### Prepare EKS cluster

Step 1: Update local kubeconfig for your EKS cluster

Use aws eks update-kubeconfig to configure your existing kubeconfig file with your hybrid nodes-enabled EKS cluster or create a new kubeconfig for your cluster. By default, the resulting configuration file is created at the default kubeconfig path (.kube) in your home directory or merged with an existing config file at that location.

You can specify another path with the --kubeconfig option. If you use the --kubeconfig option, you must use that kubeconfig file in all subsequent kubectl commands.

```
aws eks update-kubeconfig --region $REGION --name hybrid-eks-cluster
```

Step 2: Update VPC CNI with anti-affinity for hybrid nodes

The VPC CNI is not supported to run on hybrid nodes as it relies on VPC resources to configure interfaces for pods that run on EC2 nodes. In the following step, update the VPC CNI with anti-affinity for nodes labeled with the default hybrid nodes label eks.amazonaws.com/compute-type: hybrid.

```
kubectl patch ds aws-node -n kube-system --type merge --patch-file=/dev/stdin <<-EOF
spec:
  template:
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: kubernetes.io/os
                operator: In
                values:
                - linux
              - key: kubernetes.io/arch
                operator: In
                values:
                - amd64
                - arm64
              - key: eks.amazonaws.com/compute-type
                operator: NotIn
                values:
                - hybrid
EOF
```

Step 3: Map Hybrid Nodes IAM Role in Kubernetes RBAC

The Hybrid Nodes IAM Role you created previously needs to be mapped to a Kubernetes group with sufficient node permissions for hybrid nodes to be able to join the EKS cluster. In order to allow access through aws-auth ConfigMap, the EKS cluster must have been configured with API_AND_CONFIG_MAP  authentication mode, which was used in the previous steps.


Check to see if you have an existing aws-auth ConfigMap for your cluster.

```
kubectl describe configmap -n kube-system aws-auth
```

If there is not an existing aws-auth ConfigMap for your cluster, create it with the following command. Note, {{SessionName}} is the correct template formatting to save in the ConfigMap, do not replace it with other values.

First, get the `eks-hybrid-nodes-role` arn:
```
aws  iam get-role --role-name eks-hybrid-nodes-role | grep Arn
```
And include it in the command below:

```
kubectl apply -f=/dev/stdin <<-EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: aws-auth
  namespace: kube-system
data:
  mapRoles: |
    - groups:
      - system:bootstrappers
      - system:nodes
      rolearn: <ARN of the Hybrid Nodes IAM Role>
      username: system:node:{{SessionName}}
EOF
```

If you have an existing aws-auth ConfigMap for your cluster, edit it with the following command and add the mapping for your Hybrid Nodes IAM Role.

```
kubectl edit cm aws-auth -n kube-system
```

```
data:
  mapRoles: |
    - groups:
      - system:bootstrappers
      - system:nodes
      rolearn: <ARN of the Hybrid Nodes IAM Role>
      username: system:node:{{SessionName}}
```

## Prepare on-premises credential provider

### SSM hybrid activations

Setup SSM hybrid activations with the instructions below. For more information on SSM hybrid activations, reference the SSM documentation. For details on automating the SSM hybrid activation process, reference this AWS blog post.

The SSM hybrid activation code is displayed ONLY ONCE after creating the activation. Save the activation code, you will use this later during node bootstrap. If the activation code is lost, you will need to recreate a new hybrid activation.

By default, SSM hybrid activations are active for 24 hours. You can alternatively specify an --expiration-date when you create your hybrid activation in timestamp format, such as 2024-08-01T00:00:00. If you used the steps above to create the Hybrid Nodes IAM Role with SSM instructions, the name of the role is `eks-hybrid-nodes-role`.

```
aws ssm create-activation \
    --default-instance-name hybrid-ssm-node \
    --iam-role <Name of the Hybrid Nodes IAM Role> \
    --registration-limit <max number of nodes that can use this activation>
```

### IAM Roles Anywhere

For IAM Roles Anywhere to be used for authenticating hybrid nodes, you must have certificates and keys that are signed with the trust anchor root certificate created during the IAM Roles Anywhere setup process. You can create hybrid nodes certificates and keys from any machine, but the certificates and keys must be on your hybrid nodes before they are bootstrapped into your EKS cluster. Each node must have a unique certificate and key. The node certificates must be installed at /etc/iam/pki/server.pem on each hybrid node and the node keys must be installed at /etc/iam/pki/server.key on each hybrid node. You may need to create the directories before placing the certificates and keys in the directories with sudo mkdir -p /etc/iam/pki.

The script below can be used to generate certificates and keys for each node if they do not already exist. Note, this script uses node01 and node02 as the node names. These must be used in a later step as the nodeName in your nodeConfig.yaml that you will use with the hybrid nodes CLI (nodeadm) when bootstrapping your nodes in your EKS cluster.

```
echo "basicConstraints = critical, CA:FALSE
keyUsage = critical, digitalSignature" > cert.config

for nodeid in  $(seq -s " " -f %02g 1 2); do

  openssl ecparam -genkey -name secp384r1 -out node${nodeid}.key

  echo "
  [ req ]
  prompt = no
  distinguished_name = dn

  [ dn ]
  C = US
  O = AWS
  CN = node${nodeid}
  OU = YourOrganization
  " > node${nodeid}_request.config

  openssl req -new -sha512 -nodes -key node${nodeid}.key -out node${nodeid}.csr \
      -config node${nodeid}_request.config

  openssl x509 -req -sha512 -days 365 -in node${nodeid}.csr -CA rootCA.crt \
      -CAkey rootCA.key -CAcreateserial -out node${nodeid}.pem -extfile cert.config
done
```

You can validate the resulting certificates with the following.

```
openssl verify -verbose -CAfile rootCA.crt node01.pem
openssl verify -verbose -CAfile rootCA.crt node02.pem
```


## Connect Hybrid Nodes

Step 0: Install the AWS CLI on each on-premises host

```
sudo apt update
sudo apt install unzip -y
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install
```

Step 1: When using the official Packer templates to provision your ISO images into AMIs, they come prepackaged with the EKS hybrid nodes CLI (called nodeadm). **The rest of this guide assumes you've completed this step.**

Step 2: Connect hybrid nodes to your EKS cluster

Connecting hybrid nodes to your EKS cluster can be done with the included `ssm_nodeactivation_script.sh` if you've been following the SSM instructions thus far. IAM requires manual setup.

### SSM

If you're using SSM, you can activate hybrid nodes using the provided `ssm_nodeactivation_script.sh` file in this project. After running the previous activation, create a `hosts.txt` file within this same directory to provide a comma-delimited list of your nodes following the format `<Instance SSH Username>,<Instance Public IP>,<OS type as 'ubuntu' or 'redhat'>`

```
$ touch hosts.txt
```

example hosts file:
```
ubuntu,1.1.1.1,ubuntu
ec2-user,1.2.1.1,redhat
```

To run the script, enter the following:
```
$ bash ssm_nodeactivation_script.sh \
    <Path to Instance Private Key> \
    <Path to hosts.txt File> \
    <SSM-Activation-ID> \
    <SSM-Activation-Code> \
    <Cluster Name> \
    <Cluster Region>
```

### IAM

If using IAM roles, the nodeConfig.yaml file will need to be created manually on each node that contains the information your hybrid node needs to connect to the EKS cluster.

Example nodeConfig.yaml for IAM Roles Anywhere

- nodeName: The name of the node must match the CommonName (CN) of the node’s certificate. If the name does not match the CN on the certificate, the node will fail to bootstrap due to the scoped down Condition in the Hybrid Nodes IAM Role trust policy.
- roleArn: The intermediate role with permission to read from ECR and call EKS Describe Cluster.
- assumeRoleArn: The role IAM Roles Anywhere uses to assume the Hybrid Nodes IAM Role. Note, the need for this second role may be removed during the beta, such that the IAM Roles Anywhere principal will directly assume the Hybrid Nodes IAM role.
- trustAnchorArn: You can retrieve your IAM Roles Anywhere trust anchor ARN from the hybrid-beta-ira cloud-formation stack with the command:
```
aws cloudformation describe-stacks --stack-name hybrid-beta-ira --query 'Stacks[0].Outputs[?OutputKey==`IRATrustAnchorARN`].OutputValue' --output text
```
- profileArn: You can retrieve your IAM Roles Anywhere profile ARN  from the hybrid-beta-ira  cloud-formation stack with the command:
```
aws cloudformation describe-stacks --stack-name hybrid-beta-ira --query 'Stacks[0].Outputs[?OutputKey==`IRAProfileARN`].OutputValue' --output text
```

```
apiVersion: node.eks.aws/v1alpha1
kind: NodeConfig
spec:
  cluster:
    name: <cluster-name>
    region: <aws-region>
  hybrid:
    nodeName: <your-node-name>
    iamRolesAnywhere:
        trustAnchorArn: <trust-anchor-arn>
        profileArn: <profile-arn>
        roleArn: <arn:aws:iam::<ACCOUNT>:role/hybrid-beta-ira-intermediate-role>
        assumeRoleArn: <arn:aws:iam::<ACCOUNT>:role/hybrid-beta-ira-nodes>
```
Run the nodeadm init command with your nodeConfig.yaml to connect your hybrid node to your EKS cluster.

```
sudo ./nodeadm init -c file://nodeConfig.yaml
```

If the above command completes successfully, your hybrid node has joined your EKS cluster. You can verify this in the EKS console by navigating to the Compute tab for your cluster or with kubectl get nodes. Note, at this point your nodes will have status Not Ready, which is expected and is due to the lack of a CNI running on your hybrid nodes.

If your nodes did not join the cluster, check the status of the kubelet and the kubelet logs with the following commands.

```
systemctl status kubelet
```

```
journalctl -u kubelet -f
```

If you see an Unauthorized error in the kubelet logs, confirm your aws-auth ConfigMap is configured correctly and that your Hybrid Nodes IAM Role has the correct permissions. If you cannot resolve the issue, send an email with the details of your issue to aws-eks-hybrid@amazon.com.

### Install CNI

For your hybrid nodes to become Ready to serve workloads, you need to install a CNI. Run the steps in this section from your local machine or instance that you use to contact the EKS cluster’s Kubernetes API server.

### Cilium

Install the Cilium Helm repo.

```
helm repo add cilium https://helm.cilium.io/
```

Create a yaml file called `cilium-values.yaml` that enables bgpControlPlane and configures Cilium with affinity to run on hybrid nodes only. Replace the clusterPoolIPv4PodCIDRList with the remote pod cidrs you configured during EKS cluster creation. Note, this configures Cilium with bgpControlPlane which is used in a subsequent step to advertise Pod IP addresses to the on-premises network. If you do not want to use BGP, you can omit that section from the Cilium configuration. You can configure clusterPoolIPv4MaskSize based on your required pods per node, see Cilium docs.

```
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: eks.amazonaws.com/compute-type
          operator: In
          values:
          - hybrid
ipam:
  mode: cluster-pool
  operator:
    clusterPoolIPv4MaskSize: 25
    clusterPoolIPv4PodCIDRList:
    - <Pod CIDR>
operator:
  unmanagedPodWatcher:
    restart: false
bgpControlPlane:
  enabled: false
```

Install Cilium on your cluster. Note, if you are using a specific kubeconfig file, use the --kubeconfig flag.

```
helm install cilium cilium/cilium --version 1.15.6 \
--namespace kube-system --values cilium-values.yaml
```

### Uninstall

To remove a node from EKS cluster, it is recommended to drain the node to move pods to an another node before removing the node. In order to safely drain a node, run the following command. Further information on draining nodes can be found in the Kubernetes documentation. Note, if you are using a specific kubeconfig file, use the --kubeconfig flag.

```
kubectl drain --ignore-daemonsets <node-name>
```

During the beta, nodeadm does not check for pods running on the node before uninstalling components from the node. Uninstall is disruptive to workloads running on the node. Before running uninstall, ensure no pods are running except for daemonsets and static pods.

To stop and uninstall the EKS hybrid node artifacts on the node, run the following command:

```
sudo ./nodeadm uninstall
```

With the hybrid nodes artifacts stopped and uninstalled, remove the node resource from the cluster using the following command. Note, if you are using a specific kubeconfig file, use the --kubeconfig flag.

```
kubectl delete node <node-name>
```
