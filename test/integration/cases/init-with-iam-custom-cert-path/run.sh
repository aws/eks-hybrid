#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
wait::dbus-ready

mkdir -p /etc/certificates/iam/pki
touch /etc/certificates/iam/pki/my-server.crt
touch /etc/certificates/iam/pki/my-server.key

declare INITIAL_VERSION=1.30

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

nodeadm install $INITIAL_VERSION --credential-provider iam-ra

nodeadm init --skip run,ip-validation --config-source file://config.yaml
validate-file /etc/systemd/system/aws_signing_helper_update.service 644 expected-aws-signing-helper-systemd-unit
validate-file /.aws/config 644 expected-aws-config
