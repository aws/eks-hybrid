#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

NODEADM_URL="$1"
KUBERNETES_VERSION="$2"
PROVIDER="$3"
REGION="$4"
NODEADM_ADDITIONAL_ARGS="${5-}"

# nodeadmin uninstall does not remove this folder, which contains the cilium/calico config
# which kubelet uses to determine if a node is "Ready"
# if we do not remove this folder, the node will flip to ready on re-join immediately
# removing on boot instead of before rebooting to ensure containers are no longer running
# so deletion succeeds and is not replaced by the running container
rm -rf /etc/cni/net.d

if [ ! -f /usr/local/bin/nodeadm ]; then
    echo "Downloading nodeadm binary from: $NODEADM_URL"
    for i in {1..5}; do 
        curl --compressed --fail --retry 5 -L "$NODEADM_URL" -o /usr/local/bin/nodeadm && break || {
            echo "Curl attempt $i failed, retrying..."
            sleep 5
        }
    done
    if [ ! -f /usr/local/bin/nodeadm ]; then
        echo "ERROR: Failed to download nodeadm after 5 attempts"
        exit 1
    fi
    chmod +x /usr/local/bin/nodeadm
    echo "Successfully downloaded nodeadm"
fi
mv /tmp/nodeadm-wrapper.sh /tmp/nodeadm

echo "Installing kubernetes components"
# the test will wait up to 10 minutes for the node to become ready
# we give install 8 minutes, which should be more than enough, but in case it does
# timeout due to issues downloading the artifacts, the logs will more clearly indicate this as the cause of the failure
/tmp/nodeadm install $KUBERNETES_VERSION $NODEADM_ADDITIONAL_ARGS --credential-provider $PROVIDER --region $REGION --timeout 8m

echo "Initializing the node"
/tmp/nodeadm init -c file:///nodeadm-config.yaml
