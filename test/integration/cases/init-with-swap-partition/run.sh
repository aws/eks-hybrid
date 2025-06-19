#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws
mock::kubelet $CURRENT_VERSION.0
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
