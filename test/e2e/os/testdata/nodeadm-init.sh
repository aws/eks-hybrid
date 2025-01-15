#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

NODEADM_URL="$1"
KUBERNETES_VERSION="$2"
PROVDER="$3"
NODEADM_ADDITIONAL_ARGS="${4-}"

function run_debug(){
    /tmp/nodeadm debug -c file:///nodeadm-config.yaml || true
}

trap "run_debug" EXIT

echo "Downloading nodeadm binary"
for i in {1..5}; do curl --fail -s --retry 5 -L "$NODEADM_URL" -o /tmp/nodeadm && break || sleep 5; done

chmod +x /tmp/nodeadm

echo "Installing kubernetes components"
/tmp/nodeadm install $KUBERNETES_VERSION $NODEADM_ADDITIONAL_ARGS --credential-provider $PROVDER

echo "Initializing the node"
/tmp/nodeadm init -c file:///nodeadm-config.yaml
