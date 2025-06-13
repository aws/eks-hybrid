#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws

aws eks create-cluster \
    --name test-cluster \
    --region us-west-2 \
    --kubernetes-version 1.30 \
    --role-arn arn:aws:iam::123456789010:role/mockHybridNodeRole \
    --resources-vpc-config "subnetIds=subnet-1,subnet-2,endpointPublicAccess=true" \
    --remote-network-config '{"remoteNodeNetworks":[{"cidrs":["172.16.0.0/24"]}],"remotePodNetworks":[{"cidrs":["10.0.0.0/8"]}]}'

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

nodeadm install 1.30 --credential-provider iam-ra

mock::aws_signing_helper

# should fail when --node-ip set to ip not in remote node networks
if nodeadm init --skip run --config-source file://config-ip-out-of-range.yaml; then
    echo "nodeadm init should have failed with ip out of range but succeeded unexpectedly"
    exit 1
fi

# should succeed when --node-ip set to ip in remote node networks
nodeadm init --skip run --config-source file://config-ip-in-range.yaml
