#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws

aws eks create-cluster \
    --name test-cluster \
    --region us-west-2 \
    --kubernetes-version $CURRENT_VERSION \
    --role-arn arn:aws:iam::123456789010:role/mockHybridNodeRole \
    --resources-vpc-config "subnetIds=subnet-1,subnet-2,endpointPublicAccess=true" \
    --remote-network-config '{"remoteNodeNetworks":[{"cidrs":["172.16.0.0/24"]}],"remotePodNetworks":[{"cidrs":["10.0.0.0/8"]}]}'

wait::dbus-ready

# Setup IAM certificate
PKI_DIR="/etc/iam/pki"
mock::iamra-certificate-path $PKI_DIR

nodeadm install $CURRENT_VERSION --credential-provider iam-ra

mock::aws_signing_helper

# should fail when --node-ip set to ip not in remote node networks
if nodeadm init --skip run,k8s-authentication-validation --config-source file://config-ip-out-of-range.yaml; then
    echo "nodeadm init should have failed with ip out of range but succeeded unexpectedly"
    exit 1
fi

# should succeed when --node-ip set to ip in remote node networks
nodeadm init --skip run,k8s-authentication-validation --config-source file://config-ip-in-range.yaml
