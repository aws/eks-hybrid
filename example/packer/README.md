# Hybrid Nodes Packer Template Usage

This template is provided to build Ubuntu and RHEL images with the required hybrid-nodes `nodeadm` installed and ready to run. An ISO image can be provided for Ubuntu 22.04/24.04 or RHEL 8/9 and the output can be either AMI, OVA, Qcow2 or Raw.

The repo includes supplemental kickstark files for Ubuntu and RHEL that help facilitate the OS install process, taken from the [Kubernetes-Sigs Repo](https://github.com/kubernetes-sigs/image-builder/tree/main/images/capi/packer).

## Prerequisites

Before using this template, ensure you have the following:

- Packer: version 1.11.0 or higher
- Packer Plugins:
    - Amazon Plugin: Version 1.2.8 or higher
    - vSphere Plugin: Version 1.4.0 or higher
    - QEMU Plugin: Version 1.x

## Overview

This template builds images for:
- AWS: Creates Amazon Machine Images (AMI) for Ubuntu 22.04/24.04 and RHEL 8/9. You can provide your AWS CLI profile and the resulting AMI will appear in the users account.
- vSphere: Creates VMware templates using an ISO for Ubuntu and RHEL and appear in the specified folder.
- QEMU: Generates a Qcow2 or raw image for Ubuntu and RHEL.

These images have `nodeadm` installed and configured to the specifiec credential provider.


## Required Environment Variables

Set the following environment variables before running the Packer build:

## Nodeadm Architecture Type
- NODEADM_ARCH: String. Architecture of your system to install the proper nodeadm. Supports both x86_64 and ARM. Enter 'amd' or 'arm'.

## Packer SSH Password
- PKR_SSH_PASSWORD: String. Packer uses the ssh_username and ssh_password variables to SSH into the created machine when provisioning. This needs to match the passwords used to create the initial user within the respective OS's `kickstart` or `user-data` files. The default is set as "builder" or "ubuntu" depending on the OS. When setting your password, make sure to change it within the corresponding `ks.cfg` or `user-data` file as well.

## ISO Image and Checksum
Used primarily when building non-AMI images.

- ISO_URL: String. URL of the ISO to use. Can be a web link to download from a server, or an absolute path to a local file
- ISO_CHECKSUM: String. Associated checksum for the supplied ISO.

## AWS Configuration
- AWS_PROFILE: String. AWS profile for authentication.

## Credential Provider
- CREDENTIAL_PROVIDER: String. Authentication type for AWS temporary credentials. Valid values are:
    - ssm (default)
    - iam

## RHEL Subscription Manager
- RH_USERNAME: String. RHEL subscription manager username.
- RH_PASSWORD: String. RHEL subscription manager password.

## RHEL Version Number
 - RHEL_VERSION: String. Rhel iso version being used. Can be 8 or 9.

## Kubernetes Version
 - K8S_VERSION: String. Kubernetes version for hybrid nodes (for example `1.31`). For supported Kubernetes versions, see [Amazon EKS Kubernetes versions](https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html).

## vSphere Configuration
- VSPHERE_SERVER: String. vSphere server address.
- VSPHERE_USER: String. vSphere username.
- VSPHERE_PASSWORD: String. Vsphere password.
- VSPHERE_DATACENTER: String. vSphere datacenter name.
- VSPHERE_CLUSTER: String. Vsphere cluster name.
- VSPHERE_DATASTORE: String. vSphere datastore name.
- VSPHERE_NETWORK: String. vSphere network name.
- VSPHERE_OUTPUT_FOLDER: String. vSphere output folder for the templates.

## QEMU Configuration
- PACKER_OUTPUT_FORMAT: String. Output format for the QEMU builder. Valid values are:
    - qcow2
    - raw

## Setup Instructions

1. Install Packer and the required plugins.
2. Set up your environment variables:

```
export AWS_PROFILE="your-aws-profile"
export NODEADM_ARCH="amd" # Select 'amd' or 'arm'
export CREDENTIAL_PROVIDER="ssm"  # or "iam"
export RH_USERNAME="your-rhsm-username"
export RH_PASSWORD="your-rhsm-password"
export VSPHERE_SERVER="your-vsphere-server"
export VSPHERE_USER="your-vsphere-username"
export VSPHERE_PASSWORD="your-vsphere-password"
export VSPHERE_DATACENTER="your-datacenter"
export VSPHERE_CLUSTER="your-cluster"
export VSPHERE_DATASTORE="your-datastore"
export VSPHERE_NETWORK="your-network"
export VSPHERE_OUTPUT_FOLDER="your-output-folder"
export PACKER_OUTPUT_FORMAT="qcow2"  # or "raw"
export RHEL_VERSION="8" # 8 or 9
export K8S_VERSION="1.26" # Major version from 1.26 - 1.31
export PKR_SSH_PASSWORD:"ubuntu" # Change the ks.cfg or user-data file to match. Defaults are ubuntu or builder depending on OS.
export ISO_URL="" # URL of the ISO to use. Can be a web link to download from a server, or an absolute path to a local file
export ISO_CHECKSUM="" # Associated checksum for the supplied ISO.
```
3. Validate the packer template. Replace `template` with the name of you template file.
```
packer validate template.pkr.hcl
```

### Utilizing RHEL with Vsphere

In order to serve the included kickstart files with RHEL 8 and 9 on Vsphere, we need to convert it into a OEMDRV image and supply it as an ISO to boot from. You will need to manually upload the resulting ISO into the Vsphere datastore folder you specify under `iso_paths`. This guide assumes `[ YOUR_DATASTORE ] packer_cache/YOUR_RHEL_KS.iso`, you can set the `VSPHERE_DATACENTER` environment variable to set this at any time. Name your kickstart ISO as `rhel8_ks.iso` or `rhel9_ks.iso` to keep it consistent with the path used in this template. If you want to name it something else, make sure to change the filename in the path for the corresponding version.

To create a kick start ISO, you will need to download `genisoimage`:

Ubuntu, Debian
```
sudo apt-get update
sudo apt-get install genisoimage
```

CentOS, RHEL,
```
sudo yum install genisoimage
```

Arch Linux
```
sudo pacman -S cdrkit
```

Then, run the following command:

```
genisoimage -o YOUR_RHEL_KS.iso -V "OEMDRV" /PATH/TO/YOUR/KICKSTART.cfg
```



### Build Images

To build specific images, utilize the `-only` flag to leverage the general builder and specify the source needed.


### AWS Images


#### Ubuntu 22.04 AMI
```
packer build -only=general-build.amazon-ebs.ubuntu22 template.pkr.hcl
```
#### Ubuntu 24.04 AMI
```
packer build -only=general-build.amazon-ebs.ubuntu24 template.pkr.hcl
```
#### RHEL 8 AMI
```
packer build -only=general-build.amazon-ebs.rhel8 template.pkr.hcl
```
#### RHEL 9 AMI
```
packer build -only=general-build.amazon-ebs.rhel9 template.pkr.hcl
```


### vSphere Images


#### Ubuntu 22.04 OVA
```
packer build -only=general-build.vsphere-iso.ubuntu22 template.pkr.hcl
```
#### Ubuntu 24.04 OVA
```
packer build -only=general-build.vsphere-iso.ubuntu24 template.pkr.hcl
```
#### RHEL 8 OVA
```
packer build -only=general-build.vsphere-iso.rhel8 template.pkr.hcl
```
#### RHEL 9 OVA
```
packer build -only=general-build.vsphere-iso.rhel9 template.pkr.hcl
```


### QEMU Images

Note: If you are building an image for a specific host CPU that does not match your builder host, see the [QEMU documentation](https://www.qemu.org/docs/master/system/qemu-cpu-models.html) for the name that matches your host CPU and use the -cpu flag with the name of the host CPU when you run the following commands.


#### Ubuntu 22.04 Qcow2 / Raw
```
packer build -only=general-build.qemu.ubuntu22 template.pkr.hcl
```
#### Ubuntu 24.04 Qcow2 / Raw
```
packer build -only=general-build.qemu.ubuntu24 template.pkr.hcl
```
#### RHEL 8 Qcow2 / Raw
```
packer build -only=general-build.qemu.rhel8 template.pkr.hcl
```
#### RHEL 9 Qcow2 / Raw
```
packer build -only=general-build.qemu.rhel9 template.pkr.hcl
```

## Pass nodeadm configuration through user-data
You can pass configuration for nodeadm in your user-data through cloud-init to configure and automatically connect hybrid nodes to your EKS cluster at host startup. Below is an example for how to accomplish this when using VMware vSphere as the infrastructure for your hybrid nodes.

1. Install the the `govc CLI` following the instructions in the govc [readme on GitHub](https://github.com/vmware/govmomi/blob/main/govc/README.md).
2. After running the Packer build in the previous section and provisioning your template, you can clone your template to create multiple different nodes using the following. You must clone the template for each new VM you are creating that will be used for hybrid nodes. Replace the variables in the command below with the values for your environment. The VM_NAME in the command below is used as your NODE_NAME when you inject the names for your VMs via your metadata.yaml file.

```
govc vm.clone -vm "/PATH/TO/TEMPLATE" -ds="YOUR_DATASTORE" \
-on=false -template=false -folder=/FOLDER/TO/SAVE/VM "VM_NAME"
```

3. After cloning the template for each of your new VMs, create a `userdata.yaml` and `metadata.yaml`  for your VMs. Your VMs can share the same `userdata.yaml` and `metadata.yaml` and you will populate these on a per VM basis in the steps below. The nodeadm configuration is created and defined in the write_files section of your userdata.yaml. The example below uses AWS `SSM` hybrid activations as the on-premises credential provider for hybrid nodes.

**userdata.yaml**:

```
#cloud-config
users:
  - name: # username for login. Use 'builder' for RHEL or 'ubuntu' for Ubuntu.
    passwd: # password to login. Default is 'builder' for RHEL.
    groups: [adm, cdrom, dip, plugdev, lxd, sudo]
    lock-passwd: false
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash

write_files:
  - path: /usr/local/bin/nodeConfig.yaml
    permissions: '0644'
    content: |
      apiVersion: node.eks.aws/v1alpha1
      kind: NodeConfig
      spec:
          cluster:
              name: # Cluster Name
              region: # AWS region
          hybrid:
              ssm:
                  activationCode: # Your ssm activation code
                  activationId: # Your ssm activation id

runcmd:
  - /usr/local/bin/nodeadm init -c file:///usr/local/bin/nodeConfig.yaml >> /var/log/nodeadm-init.log 2>&1
```

**metadata.yaml**

Create a metadata.yaml for your environment. Keep the "$NODE_NAME" variable format in the file as this will be populated with values in a subsequent step.

```
instance-id: "$NODE_NAME"
local-hostname: "$NODE_NAME"
network:
  version: 2
  ethernets:
    nics:
      match:
        name: ens*
      dhcp4: yes
```

4. Add the `userdata.yaml` and `metadata.yaml` files as `gzip+base64` strings with the following commands. The following commands should be run for each of the VMs you are creating. Replace VM_NAME with the name of the VM you are updating.

```
export NODE_NAME="VM_NAME"
export USER_DATA=$(gzip -c9 <userdata.yaml | base64)

govc vm.change -dc="YOUR_DATASTORE" -vm "$NODE_NAME" -e guestinfo.userdata="${USER_DATA}"
govc vm.change -dc="YOUR_DATASTORE" -vm "$NODE_NAME" -e guestinfo.userdata.encoding=gzip+base64

envsubst '$NODE_NAME' < metadata.yaml > metadata.yaml.tmp
export METADATA=$(gzip -c9 <metadata.yaml.tmp | base64)

govc vm.change -dc="YOUR_DATASTORE" -vm "$NODE_NAME" -e guestinfo.metadata="${METADATA}"
govc vm.change -dc="YOUR_DATASTORE" -vm "$NODE_NAME" -e guestinfo.metadata.encoding=gzip+base64
```

5. Power on your new VMs, which should automatically connect to the EKS cluster you configured.

```
govc vm.power -on "${NODE_NAME}"
```
