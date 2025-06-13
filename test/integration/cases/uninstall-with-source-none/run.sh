#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
wait::dbus-ready

# Define certificate and key paths
PKI_DIR="/etc/iam/pki"
CERT="$PKI_DIR/server.pem"
KEY="$PKI_DIR/server.key"

# Create directory if it doesn't exist
mkdir -p $PKI_DIR

# Create empty certificate and key files
touch $CERT
touch $KEY

# Generate self-signed certificate and key using OpenSSL
openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
    -keyout $KEY \
    -out $CERT \
    -subj "/C=US/ST=Washington/L=Seattle/O=DummyOrg/CN=DummyCN"

# Set appropriate permissions
chmod 644 $CERT
chmod 600 $KEY

nodeadm install 1.30 --credential-provider iam-ra --containerd-source none
assert::files-equal /opt/nodeadm/tracker expected-nodeadm-tracker

# mock iam-ra update service credentials file
mock::iamra_aws_credentials
nodeadm init --skip run,node-ip-validation --config-source file://config.yaml

nodeadm uninstall --skip run,node-validation,pod-validation

assert::path-exists /usr/bin/containerd

# run a second test that removes the containerd from the tracker file to
# simulate older installations which would not have included none in the source
# to ensure during unmarshal it defaults to none
nodeadm install 1.30 --credential-provider iam-ra --containerd-source none
yq -i '.Artifacts.Containerd = ""' /opt/nodeadm/tracker

# mock iam-ra update service credentials file
mock::iamra_aws_credentials
nodeadm init --skip run,node-ip-validation --config-source file://config.yaml

nodeadm uninstall --skip run,node-validation,pod-validation

assert::path-exists /usr/bin/containerd