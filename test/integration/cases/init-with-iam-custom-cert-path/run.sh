#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws
wait::dbus-ready

mkdir -p /etc/certificates/iam/pki
touch /etc/certificates/iam/pki/my-server.crt
touch /etc/certificates/iam/pki/my-server.key

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

nodeadm install $CURRENT_VERSION --credential-provider iam-ra

mock::iamra_aws_credentials
nodeadm init --skip run,node-ip-validation,aws-auth-validation,k8s-endpoint-network-validation,k8s-authentication-validation --config-source file://config.yaml

validate-file /etc/systemd/system/aws_signing_helper_update.service 644 expected-aws-signing-helper-systemd-unit
validate-file /.aws/config 644 expected-aws-config
