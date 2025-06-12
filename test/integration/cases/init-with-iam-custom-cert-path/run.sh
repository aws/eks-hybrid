#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws
wait::dbus-ready

# Define certificate and key paths
PKI_DIR="/etc/certificates/iam/pki"
CERT="$PKI_DIR/my-server.crt"
KEY="$PKI_DIR/my-server.key"
mock::iamra-certificate-path $PKI_DIR $CERT $KEY

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

nodeadm install $CURRENT_VERSION --credential-provider iam-ra

# mock iam-ra update service credentials file
mock::iamra_aws_credentials

# Temporary credential validation paths
VALIDATION_CERT="$PKI_DIR/my-server_cert_validation.crt"
VALIDATION_KEY="$PKI_DIR/my-server_key_validation.key"

# Test 1: Verify that nodeadm init fails when the certificate doesn't exist
if nodeadm init --skip run,node-ip-validation --config-source file://config-certificate-validation.yaml; then
    echo "nodeadm init should have failed with iam-roles-anywhere certificate not exist but succeeded unexpectedly"
    exit 1
fi

# Test 2: Verify that INIT fails when the certificate is empty
touch $VALIDATION_CERT
touch $VALIDATION_KEY
if nodeadm init --skip run,node-ip-validation --config-source file://config-certificate-validation.yaml; then
    echo "nodeadm init should have failed with iam-roles-anywhere certificate file empty but succeeded unexpectedly"
    exit 1
fi
rm $VALIDATION_CERT
rm $VALIDATION_KEY


# Test 3: Verify that INIT fails when the certificate is corrupted by adding random data
echo "CORRUPTED_DATA" >> $VALIDATION_CERT
cp $KEY $VALIDATION_KEY
if nodeadm init --skip run,node-ip-validation --config-source file://config-certificate-validation.yaml; then
    echo "nodeadm init should have failed with iam-roles-anywhere certificate wrong file but succeeded unexpectedly"
    exit 1
fi
rm $VALIDATION_CERT
rm $VALIDATION_KEY

# Test 4: Verify that init fails when the certificate is corrupted by modifying the content
VALIDATION_CERT="$PKI_DIR/my-server_cert_validation.crt"
cp $CERT $VALIDATION_CERT
cp $KEY $VALIDATION_KEY
sed -i '2s/$/A/' "$VALIDATION_CERT"
if nodeadm init --skip run,node-ip-validation --config-source file://config-certificate-validation.yaml; then
    echo "nodeadm init should have failed with iam-roles-anywhere certificate with corrupted file but succeeded unexpectedly"
    exit 1
fi
rm $VALIDATION_CERT
rm $VALIDATION_KEY

# Success case
nodeadm init --skip run,node-ip-validation --config-source file://config.yaml
validate-file /etc/systemd/system/aws_signing_helper_update.service 644 expected-aws-signing-helper-systemd-unit
validate-file /.aws/config 644 expected-aws-config
