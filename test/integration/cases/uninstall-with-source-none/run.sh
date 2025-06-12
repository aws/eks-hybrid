#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

mock::aws
wait::dbus-ready

mkdir -p /etc/iam/pki
touch /etc/iam/pki/server.pem
touch /etc/iam/pki/server.key

nodeadm install $CURRENT_VERSION --credential-provider iam-ra --containerd-source none
assert::files-equal /opt/nodeadm/tracker expected-nodeadm-tracker

# mock iam-ra update service credentials file
mock::iamra_aws_credentials
nodeadm init --skip run,node-ip-validation --config-source file://config.yaml

nodeadm uninstall --skip run,node-validation,pod-validation

assert::path-exists /usr/bin/containerd

# run a second test that removes the containerd from the tracker file to
# simulate older installations which would not have included none in the source
# to ensure during unmarshal it defaults to none
nodeadm install $CURRENT_VERSION --credential-provider iam-ra --containerd-source none
yq -i '.Artifacts.Containerd = ""' /opt/nodeadm/tracker

# mock iam-ra update service credentials file
mock::iamra_aws_credentials
nodeadm init --skip run,node-ip-validation --config-source file://config.yaml

nodeadm uninstall --skip run,node-validation,pod-validation

assert::path-exists /usr/bin/containerd
