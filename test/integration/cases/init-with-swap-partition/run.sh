#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws
mock::kubelet $CURRENT_VERSION.0
wait::dbus-ready

# Setup IAM certificate
PKI_DIR="/etc/iam/pki"
mock::iamra-certificate-path $PKI_DIR

nodeadm install $CURRENT_VERSION  --credential-provider iam-ra

mount --bind $(pwd)/swaps-partition /proc/swaps
assert::path-exists /usr/bin/containerd

exit_code=0
STDERR=$(nodeadm init --config-source file://config.yaml --skip node-ip-validation 2>&1) || exit_code=$?
if [ $exit_code -ne 0 ]; then
    assert::is-substring "$STDERR" "partition type swap found on the host"
else
    echo "nodeadm init should have failed with: partition type swap found on the host"
    exit 1
fi

mount --bind $(pwd)/swaps-file /proc/swaps
if ! nodeadm init --skip run,node-ip-validation --config-source file://config.yaml; then
    echo "nodeadm should have successfully completed init"
    exit 1
fi

# Check if swap has been disabled and partition removed from /etc/fstab
assert::file-not-contains /etc/fstab "swap"
assert::swap-disabled-validate-path
