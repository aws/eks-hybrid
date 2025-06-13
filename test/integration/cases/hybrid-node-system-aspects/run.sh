#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
mock::kubelet 1.30.0
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

# install, enable and start firewalld to test ports aspect
dnf install -y firewalld
systemctl enable firewalld
systemctl start firewalld

# allow cilium vxlan
firewall-cmd --permanent --add-port=4789/udp

nodeadm init --skip run,install-validation,node-ip-validation --config-source file://config.yaml

# Check if aws config file has been created as specifed in NodeConfig
assert::files-equal /.aws/config expected-aws-config

# Check if sysctl aspect has been setup
assert::files-equal /etc/sysctl.d/99-nodeadm.conf expected-99-nodeadm.conf

# Check if swap has been disabled and partition removed from /etc/fstab
assert::file-not-contains /etc/fstab "swap"
assert::swap-disabled-validate-path

# Check if port has been allowed by firewalld
assert::allowed-by-firewalld "10250" "tcp"
assert::allowed-by-firewalld "10256" "tcp"
assert::allowed-by-firewalld "30000-32767" "tcp"

exit_code=0
systemctl stop firewalld
STDERR=$(nodeadm init --skip run,install-validation --config-source file://config.yaml 2>&1) || exit_code=$?
if [ $exit_code -ne 0]; then
  echo "nodeadm init should not fail with firewall-cmd installed but firewalld service not running"
  exit 1
fi
