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

# Setup IAM certificate
PKI_DIR="/etc/iam/pki"
mock::iamra-certificate-path $PKI_DIR

# remove previously installed containerd 
dnf remove -y containerd

# before running install/upgrade, install a non-latest version of containerd
# so we can ensure install nor upgrade trigger a yum upgrade of the packages
install-previous-containerd-version
generate::birth-file /usr/bin/containerd

# Test nodeadm upgrade with iam as credential provider
nodeadm install $INITIAL_VERSION --credential-provider iam-ra --containerd-source none
assert::files-equal /opt/nodeadm/tracker expected-nodeadm-tracker
assert::birth-match /usr/bin/containerd

# mock iam-ra update service credentials file
mock::iamra_aws_credentials
nodeadm init --skip run,node-ip-validation,aws-auth-validation,k8s-endpoint-network-validation --config-source file://config.yaml

# In integration test environment, the aws_signing_helper_update will run
# but stuck in a loop of failure which prevents the next nodeadm init call
# from starting it, we manually stop and reset the service to work around it.
systemctl stop aws_signing_helper_update.service
systemctl disable aws_signing_helper_update.service
systemctl daemon-reload
systemctl reset-failed

nodeadm upgrade $TARGET_VERSION --skip run,pod-validation,node-validation,init-validation,node-ip-validation,aws-auth-validation,k8s-endpoint-network-validation --config-source file://config.yaml

assert::birth-match /usr/bin/containerd
