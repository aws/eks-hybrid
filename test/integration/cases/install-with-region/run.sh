#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

VERSION=1.31

mock::aws
wait::dbus-ready
# block access to us-west-2 ssm install url
echo "0.0.0.0 amazon-ssm-us-west-2.s3.us-west-2.amazonaws.com" >> /etc/hosts

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

output=$(nodeadm install $VERSION --credential-provider ssm --region us-east-1 2>&1)
assert::output-contains-ssm-url "$output" "us-east-1"

assert::path-exists /usr/bin/containerd
assert::path-exists /usr/sbin/iptables
assert::path-exists /usr/bin/kubelet
assert::path-exists /usr/local/bin/kubectl
VERSION_INFO=$(/usr/local/bin/kubectl version || true)
assert::is-substring "$VERSION_INFO" "v$VERSION"
assert::path-exists /opt/cni/bin/
assert::path-exists /etc/eks/image-credential-provider/ecr-credential-provider
assert::path-exists /usr/local/bin/aws-iam-authenticator

assert::path-exists /opt/ssm/ssm-setup-cli
assert::path-exists /usr/bin/amazon-ssm-agent

assert::files-equal /opt/nodeadm/tracker expected-nodeadm-tracker

nodeadm uninstall --skip node-validation,pod-validation

assert::path-not-exist /usr/bin/containerd
assert::path-not-exist /usr/sbin/iptables
assert::path-not-exist /usr/bin/kubelet
assert::path-not-exist /usr/local/bin/kubectl
assert::path-not-exist /opt/cni/bin/
assert::path-not-exist /etc/eks/image-credential-provider/ecr-credential-provider
assert::path-not-exist /usr/local/bin/aws-iam-authenticator
assert::path-not-exist /usr/bin/containerd
assert::path-not-exist /opt/ssm/ssm-setup-cli
assert::path-not-exist /usr/bin/amazon-ssm-agent
assert::path-not-exist /opt/nodeadm/tracker

# Check that an invalid region name does not succeed
if nodeadm install $VERSION --credential-provider ssm --region "bad-region-name" >/dev/null 2>&1; then
    echo "Install unexpectedly succeeded with --region 'bad-region-name'"
    exit 1
fi
nodeadm uninstall --skip node-validation,pod-validation

# Check that the default region us-west-2 does not succeed
if nodeadm install $VERSION --credential-provider ssm  >/dev/null 2>&1; then
    echo "Install unexpectedly succeeded with default region us-west-2"
    exit 1
fi
