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

mkdir -p /etc/iam/pki
touch  /etc/iam/pki/server.pem
touch  /etc/iam/pki/server.key

nodeadm install 1.30 --credential-provider iam-ra

mock::aws_signing_helper

# should fail when --node-ip set to ip not in remote node networks
if nodeadm init --skip run --config-source file://config-ip-out-of-range.yaml; then
    echo "nodeadm init should have failed with ip out of range but succeeded unexpectedly"
    exit 1
fi

# should succeed when --node-ip set to ip in remote node networks
nodeadm init --skip run --config-source file://config-ip-in-range.yaml
