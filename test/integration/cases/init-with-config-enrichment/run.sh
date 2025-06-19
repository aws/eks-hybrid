#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws
wait::dbus-ready

export AWS_ENDPOINT_URL=http://localhost:5000

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

nodeadm install $CURRENT_VERSION  --credential-provider iam-ra

mock::aws_signing_helper

exit_code=0
STDERR=$(nodeadm init --skip run,node-ip-validation --config-source file://config.yaml 2>&1) || exit_code=$?
if [ $exit_code -ne 0 ]; then
    assert::is-substring "$STDERR" "ResourceNotFoundException"
else
    echo "nodeadm init should have failed while cluster does not exist"
    exit 1
fi

aws eks create-cluster \
    --name my-cluster \
    --region us-west-2 \
    --kubernetes-version $CURRENT_VERSION \
    --role-arn arn:aws:iam::123456789012:role/eksClusterRole-12-3 \
    --resources-vpc-config subnetIds=subnet-123456789012,subnet-123456789013,securityGroupIds=sg-123456789014,endpointPrivateAccess=true,endpointPublicAccess=false \
    --remote-network-config '{"remoteNodeNetworks":[{"cidrs":["10.100.0.0/16"]}],"remotePodNetworks":[{"cidrs":["10.101.0.0/16"]}]}'

if ! nodeadm init --skip run,node-ip-validation --config-source file://config.yaml; then
    echo "nodeadm init should have succeeded after creating the cluster"
    exit 1
fi
