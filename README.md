# Amazon EKS Hybrid Nodes
With [EKS Hybrid Nodes](https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-overview.html), you can use your on-premises and edge infrastructure as nodes in EKS clusters. The EKS Hybrid Nodes CLI (nodeadm) used for hybrid nodes lifecycle management differs from the nodeadm version used for bootstrapping EC2 instances as nodes in EKS clusters. You should not use the hybrid nodes nodeadm version for nodes running on EC2 instances. This repository is for the hybrid nodes nodeadm version. For the nodeadm version for EC2 instances, see the EKS AMI [GitHub repository](https://github.com/awslabs/amazon-eks-ami) and [documentation](https://awslabs.github.io/amazon-eks-ami/nodeadm/). 

## nodeadm

You can run nodeadm on each on-premises host to simplify the installation, configuration, registration, and uninstall of the hybrid nodes components. You can alternatively include nodeadm in your operating system images to automate hybrid node bootstrap (see [Packer examples](example/packer) for more information).

**See [Hybrid Nodes nodeadm reference](https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html) in the EKS User Guide for the full nodeadm usage reference. This readme contains example commands only.**

---

### Usage

#### Download nodeadm

To install nodeadm on each on-premises host, you can run the following command from your on-premises hosts.

For x86 hosts:

```sh
curl -OL 'https://hybrid-assets.eks.amazonaws.com/latest/bin/linux/amd64/nodeadm'
```

For ARM hosts

```sh
curl -OL 'https://hybrid-assets.eks.amazonaws.com/latest/bin/linux/arm64/nodeadm'
```

Add executable file permission to the downloaded binary on each host. You must run nodeadm with a user that has root/sudo privileges.

```sh
chmod +x nodeadm
```

#### nodeadm install

The `install` command is used to install the artifacts and dependencies required to run and join hybrid nodes to an EKS cluster. The install command can be run individually on each hybrid node or can be run during image build pipelines to preinstall the hybrid nodes dependencies in operating system images. You must run nodeadm with a user that has root/sudo privileges.

Install Kubernetes version 1.31 with AWS Systems Manager (SSM) as the credential provider
```sh
nodeadm install 1.31 --credential-provider ssm 
```
Install Kubernetes version 1.31 with AWS Systems Manager (SSM) as the credential provider with a download timeout of 20 minutes.
```sh
nodeadm install 1.31 --credential-provider ssm --download-timeout 20m
```
Install Kubernetes version 1.31 with AWS IAM Roles Anywhere as the credential provider
```sh
nodeadm install 1.31 --credential-provider iam-ra
```

#### nodeadm init
The `nodeadm init` command starts and connects hybrid nodes with the configured Amazon EKS cluster.

```
nodeadm init --config-source file:///root/nodeConfig.yaml
```

#### nodeadm upgrade
The `nodeadm upgrade` command shuts down the existing older Kubernetes components running on the hybrid node, uninstalls the existing older Kubernetes components, installs the new target Kubernetes components, and starts the new target Kubernetes components. It is strongly recommend to upgrade one node at a time to minimize impact to applications running on the hybrid nodes. The duration of this process depends on your network bandwidth and latency.

See [Upgrade hybrid nodes](https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-upgrade.html) in the EKS User Guide for detailed information and guidance on hybrid nodes upgrades.

Upgrade to Kubernetes version 1.31
```sh
nodeadm upgrade 1.31 --config-source file:///root/nodeConfig.yaml
```
Upgrade to Kubernetes version `1.31` with a download timeout of 20 minutes.
```sh
nodeadm upgrade 1.31 --config-source file:///root/nodeConfig.yaml --download-timeout 20m
```

#### nodeadm uninstall
The `nodeadm uninstall` command stops and removes the artifacts nodeadm installs during `nodeadm install`, including the kubelet and containerd. Note, the `nodeadm uninstall` command does not drain or delete your hybrid nodes from your cluster. You must run the drain and delete operations separately, see [Delete hybrid nodes](https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-delete.html) in the EKS User Guide for more information. 

Uninstall nodeadm-installed components
```sh
nodeadm uninstall
```
Uninstall nodeadm-installed components and skip node and pod validations
```sh
nodeadm uninstall --skip node-validation,pod-validation
```

---

### Configuration

Sample `nodeConfig.yaml` when using AWS SSM hybrid activations for hybrid nodes credentials

```yaml
apiVersion: node.eks.aws/v1alpha1
kind: NodeConfig
spec:
  cluster:
    name:             # Name of the EKS cluster
    region:           # AWS Region where the EKS cluster resides
  hybrid:
    ssm:
      activationCode: # SSM hybrid activation code
      activationId:   # SSM hybrid activation id
```

Sample `nodeConfig.yaml` for AWS IAM Roles Anywhere for hybrid nodes credentials.

```yaml
apiVersion: node.eks.aws/v1alpha1
kind: NodeConfig
spec:
  cluster:
    name:             # Name of the EKS cluster
    region:           # AWS Region where the EKS cluster resides
  hybrid:
    nodeName:         # Name of the node
    iamRolesAnywhere:
      trustAnchorArn: # ARN of the IAM Roles Anywhere trust anchor
      profileArn:     # ARN of the IAM Roles Anywhere profile
      roleArn:        # ARN of the Hybrid Nodes IAM role
```

**Kubelet configuration**: You can pass kubelet configuration and flags in your nodeadm configuration. See the example below for how to add an additional node label `abc.amazonaws.com/test-label` and config for setting `shutdownGracePeriod` to 30 seconds.

```yaml
apiVersion: node.eks.aws/v1alpha1
kind: NodeConfig
spec:
  cluster:
    name:             # Name of the EKS cluster
    region:           # AWS Region where the EKS cluster resides
  kubelet:
    config:           # Map of kubelet config and values
       shutdownGracePeriod: 30s
    flags:            # List of kubelet flags
       - --node-labels=abc.company.com/test-label=true
  hybrid:
    ssm:
      activationCode: # SSM hybrid activation code
      activationId:   # SSM hybrid activation id
```

**Containerd configuration**: You can pass custom containerd configuration in your nodeadm configuration. The containerd configuration for nodeadm accepts in-line TOML. See the example below for how to configure containerd to disable deletion of unpacked image layers in the containerd content store. 

```yaml
apiVersion: node.eks.aws/v1alpha1
kind: NodeConfig
spec:
  cluster:
    name:             # Name of the EKS cluster
    region:           # AWS Region where the EKS cluster resides
  containerd:
    config: |         # Inline TOML containerd additional configuration
       [plugins."io.containerd.grpc.v1.cri".containerd]
       discard_unpacked_layers = false
  hybrid:
    ssm:
      activationCode: # SSM hybrid activation code
      activationId:   # SSM hybrid activation id
```

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This project is licensed under the Apache-2.0 License.
