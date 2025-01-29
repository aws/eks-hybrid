#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
wait::dbus-ready

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

declare SUPPORTED_VERSIONS=(1.26 1.27 1.28 1.29 1.30)

for VERSION in ${SUPPORTED_VERSIONS}
do

    nodeadm install $VERSION --credential-provider ssm --region us-east-1

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

    assert::install-fails-with-region $VERSION "bad-region-name"
done
