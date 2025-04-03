#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
wait::dbus-ready

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

nodeadm install 1.31 --credential-provider ssm

mock::ssm
nodeadm init --skip run,preprocess,node-ip-validation --config-source file://config.yaml

assert::path-exists /root/.aws
assert::path-exists /eks-hybrid/.aws

# remove ssm registration so ssm skips trying to deregister during uninstall
rm /var/lib/amazon/ssm/registration
nodeadm uninstall --skip node-validation,pod-validation

# Verify AWS config and symlink are removed after uninstall
assert::path-not-exist /root/.aws
assert::path-not-exist /eks-hybrid/.aws
assert::path-not-exist /usr/bin/ssm-agent-worker
assert::path-not-exist /etc/amazon
assert::path-not-exist /var/lib/amazon/ssm/registration

