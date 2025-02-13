#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
wait::dbus-ready

declare INITIAL_VERSION=1.26
declare TARGET_VERSION=1.30

mkdir -p /etc/iam/pki
touch /etc/iam/pki/server.pem
touch /etc/iam/pki/server.key

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

# Test nodeadm upgrade with iam as credential provider
# initial: version 1.26
# target: version 1.30
nodeadm install $INITIAL_VERSION --credential-provider iam-ra

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
assert::path-exists /usr/local/bin/aws_signing_helper
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

nodeadm init --skip run,ip-validation --config-source file://config.yaml
validate-file /etc/systemd/system/aws_signing_helper_update.service 644 expected-aws-signing-helper-systemd-unit
validate-file /.aws/config 644 expected-aws-config
# The memory reserved by kubelet is dynamic depending on the host that builts the docker image
# Remove kubeReserved field before checking its content
cat <<< $(jq 'del(.kubeReserved)' /etc/kubernetes/kubelet/config.json) > /etc/kubernetes/kubelet/config.json
validate-json-file /etc/kubernetes/kubelet/config.json 644 expected-kubelet-config-initial
validate-file /etc/containerd/config.toml 644 expected-containerd-config
validate-file /var/lib/kubelet/kubeconfig 644 expected-kubeconfig
validate-json-file /etc/eks/image-credential-provider/config.json 644 expected-image-credential-provider-config-initial
validate-file /etc/kubernetes/pki/ca.crt 644 expected-ca-crt
# Order of items in this file is random, skip checking content of /etc/eks/kubelet/environment
validate-file /etc/eks/kubelet/environment 644

# In integration test environment, the aws_signing_helper_update will run
# but stuck in a loop of failure which prevents the next nodeadm init call
# from starting it, we manually stop and reset the service to work around it.
systemctl stop aws_signing_helper_update.service
systemctl disable aws_signing_helper_update.service
systemctl daemon-reload
systemctl reset-failed

nodeadm upgrade $TARGET_VERSION --skip run,pod-validation,node-validation,init-validation --config-source file://config.yaml

assert::path-exists /usr/bin/containerd
assert::path-exists /usr/sbin/iptables
assert::path-exists /usr/local/bin/kubectl
VERSION_INFO=$(/usr/local/bin/kubectl version || true)
assert::is-substring "$VERSION_INFO" "v$TARGET_VERSION"
assert::path-exists /opt/cni/bin/
assert::path-exists /etc/eks/image-credential-provider/ecr-credential-provider
assert::path-exists /usr/local/bin/aws-iam-authenticator
assert::path-exists /usr/local/bin/aws_signing_helper
assert::files-equal /opt/nodeadm/tracker expected-nodeadm-tracker
assert::path-exists /etc/systemd/system/kubelet.service
assert::files-equal /etc/systemd/system/kubelet.service expected-kubelet-systemd-unit

assert::file-permission-matches /usr/local/bin/aws_signing_helper 755
assert::file-permission-matches /usr/bin/kubelet 755
assert::file-permission-matches /etc/systemd/system/kubelet.service 644
assert::file-permission-matches /usr/local/bin/kubectl 755
assert::file-permission-matches /etc/eks/image-credential-provider/ecr-credential-provider 755
assert::file-permission-matches /usr/local/bin/aws-iam-authenticator 755

validate-file /etc/systemd/system/aws_signing_helper_update.service 644 expected-aws-signing-helper-systemd-unit
validate-file /.aws/config 644 expected-aws-config
cat <<< $(jq 'del(.kubeReserved)' /etc/kubernetes/kubelet/config.json) > /etc/kubernetes/kubelet/config.json
validate-json-file /etc/kubernetes/kubelet/config.json 644 expected-kubelet-config-upgraded
validate-file /etc/containerd/config.toml 644 expected-containerd-config
validate-file /var/lib/kubelet/kubeconfig 644 expected-kubeconfig
validate-json-file /etc/eks/image-credential-provider/config.json 644 expected-image-credential-provider-config-upgraded
validate-file /etc/kubernetes/pki/ca.crt 644 expected-ca-crt
validate-file /etc/eks/kubelet/environment 644
