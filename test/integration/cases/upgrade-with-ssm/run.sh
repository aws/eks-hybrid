#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws
wait::dbus-ready

# run upgrade test upgrading from initial version to target version
declare INITIAL_VERSION=$DEFAULT_INITIAL_VERSION
declare TARGET_VERSION=$CURRENT_VERSION

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

# Test nodeadm upgrade with ssm as credential provider
nodeadm install $INITIAL_VERSION --credential-provider ssm
# Verify all binaries are installed at correct location
# and all generated config files have desired content
assert::path-exists /usr/bin/containerd
assert::path-exists /usr/sbin/iptables
assert::path-exists /usr/bin/kubelet
assert::path-exists /usr/local/bin/kubectl
VERSION_INFO=$(/usr/local/bin/kubectl version || true)
assert::is-substring "$VERSION_INFO" "v$INITIAL_VERSION"
assert::path-exists /opt/cni/bin/
assert::path-exists /etc/eks/image-credential-provider/ecr-credential-provider
assert::path-exists /usr/local/bin/aws-iam-authenticator
assert::path-exists /opt/ssm/ssm-setup-cli
assert::path-exists /usr/bin/amazon-ssm-agent
assert::files-equal /opt/nodeadm/tracker expected-nodeadm-tracker
assert::path-exists /etc/systemd/system/kubelet.service
assert::files-equal /etc/systemd/system/kubelet.service expected-kubelet-systemd-unit
# Verify installed binaries have the correct permission as we specified in code
# Only verify the files whose permission we specified in code
assert::file-permission-matches /usr/bin/kubelet 755
assert::file-permission-matches /etc/systemd/system/kubelet.service 644
assert::file-permission-matches /usr/local/bin/kubectl 755
assert::file-permission-matches /etc/eks/image-credential-provider/ecr-credential-provider 755
assert::file-permission-matches /usr/local/bin/aws-iam-authenticator 755
assert::file-permission-matches /opt/ssm/ssm-setup-cli 755

mock::ssm
nodeadm init --skip run,preprocess,node-ip-validation,k8s-endpoint-network-validation,k8s-authentication-validation --config-source file://config.yaml

# The memory reserved by kubelet is dynamic depending on the host that builts the docker image
# Remove kubeReserved field before checking its content
cat <<< $(jq 'del(.kubeReserved)' /etc/kubernetes/kubelet/config.json) > /etc/kubernetes/kubelet/config.json
validate-json-file /etc/kubernetes/kubelet/config.json 644 expected-kubelet-config-initial
validate-file /var/lib/kubelet/kubeconfig 644 expected-kubeconfig
validate-file /etc/containerd/config.toml 644 expected-containerd-config
validate-json-file /etc/eks/image-credential-provider/config.json 644 expected-image-credential-provider-config
validate-file /etc/kubernetes/pki/ca.crt 644 expected-ca-crt
# Order of items in this file is random, skip checking content of /etc/eks/kubelet/environment
validate-file /etc/eks/kubelet/environment 644

# Since we are upgrading kubernetes version primarily also check if the checksums of artifacts changed
generate::birth-file /usr/bin/kubelet
generate::birth-file /usr/local/bin/kubectl

# Generate birth stat files for artifacts that we dont expect to change
generate::birth-file /usr/bin/containerd
generate::birth-file /usr/sbin/iptables
generate::birth-file /usr/bin/amazon-ssm-agent

# Create dummy cilium-cni to ensure cilium isnt getting replaced
touch /opt/cni/cilium-cni

nodeadm upgrade $TARGET_VERSION --skip run,preprocess,pod-validation,node-validation,init-validation,node-ip-validation,k8s-endpoint-network-validation,k8s-authentication-validation --config-source file://config.yaml

assert::birth-not-match /usr/bin/kubelet
assert::birth-not-match /usr/local/bin/kubectl
assert::birth-match /usr/bin/containerd
assert::birth-match /usr/sbin/iptables
assert::birth-match /usr/bin/amazon-ssm-agent
assert::path-exists /opt/cni/cilium-cni

assert::path-exists /usr/bin/containerd
assert::path-exists /usr/sbin/iptables
assert::path-exists /usr/local/bin/kubectl
VERSION_INFO=$(/usr/local/bin/kubectl version || true)
assert::is-substring "$VERSION_INFO" "v$TARGET_VERSION"
assert::path-exists /opt/cni/bin/
assert::path-exists /etc/eks/image-credential-provider/ecr-credential-provider
assert::path-exists /usr/local/bin/aws-iam-authenticator
assert::path-exists /opt/ssm/ssm-setup-cli
assert::path-exists /usr/bin/amazon-ssm-agent
assert::files-equal /opt/nodeadm/tracker expected-nodeadm-tracker
assert::path-exists /etc/systemd/system/kubelet.service
assert::files-equal /etc/systemd/system/kubelet.service expected-kubelet-systemd-unit

assert::file-permission-matches /usr/bin/kubelet 755
assert::file-permission-matches /etc/systemd/system/kubelet.service 644
assert::file-permission-matches /usr/local/bin/kubectl 755
assert::file-permission-matches /etc/eks/image-credential-provider/ecr-credential-provider 755
assert::file-permission-matches /usr/local/bin/aws-iam-authenticator 755
assert::file-permission-matches /opt/ssm/ssm-setup-cli 755

cat <<< $(jq 'del(.kubeReserved)' /etc/kubernetes/kubelet/config.json) > /etc/kubernetes/kubelet/config.json
validate-json-file /etc/kubernetes/kubelet/config.json 644 expected-kubelet-config-upgraded
validate-file /var/lib/kubelet/kubeconfig 644 expected-kubeconfig
validate-file /etc/containerd/config.toml 644 expected-containerd-config
validate-file /etc/kubernetes/pki/ca.crt 644 expected-ca-crt
# Order of items in this file is random, skip checking content of /etc/eks/kubelet/environment
validate-file /etc/eks/kubelet/environment 644

# Perform another upgrade to same TARGET_VERSION which would result in all artifacts not getting upgraded
# and exiting. Validate artifacts were not upgraded/changed
generate::birth-file /usr/bin/kubelet
generate::birth-file /usr/local/bin/kubectl
generate::birth-file /etc/eks/image-credential-provider/ecr-credential-provider

nodeadm upgrade $TARGET_VERSION --skip run,pod-validation,node-validation,init-validation,k8s-endpoint-network-validation,k8s-authentication-validation --config-source file://config.yaml
assert::birth-match /usr/bin/kubelet
assert::birth-match /usr/local/bin/kubectl
assert::birth-match /usr/bin/containerd
assert::birth-match /usr/sbin/iptables
assert::birth-match /usr/bin/amazon-ssm-agent
assert::birth-match /etc/eks/image-credential-provider/ecr-credential-provider
