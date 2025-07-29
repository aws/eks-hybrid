#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh
source /test-constants.sh

mock::aws
wait::dbus-ready

# remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

# Install a version to test uninstall
nodeadm install $CURRENT_VERSION --credential-provider ssm

# Create some test files in directories that should be cleaned up by force
mkdir -p /var/lib/kubelet/test
echo "test" > /var/lib/kubelet/test/file
echo "test" > /var/lib/kubelet/kubeconfig
mkdir -p /etc/kubernetes/test
echo "test" > /etc/kubernetes/test/file
mkdir -p /var/lib/cni/test
echo "test" > /var/lib/cni/test/file
mkdir -p /etc/cni/net.d/test
echo "test" > /etc/cni/net.d/test/file

# First uninstall without force - these directories should remain
nodeadm uninstall --skip node-validation,pod-validation

# These two files are removed even without force
assert::path-not-exist /etc/kubernetes/test/file
assert::path-not-exist /var/lib/kubelet/kubeconfig

assert::path-exists /var/lib/kubelet
assert::path-exists /var/lib/kubelet/test/file
assert::path-exists /var/lib/cni/test/file
assert::path-exists /etc/cni/net.d/test/file

# Install again to test force uninstall
nodeadm install $CURRENT_VERSION --credential-provider ssm

# Recreate test files
mkdir -p /var/lib/kubelet/test
echo "test" > /var/lib/kubelet/test/file
mkdir -p /etc/kubernetes/test
echo "test" > /etc/kubernetes/test/file
mkdir -p /var/lib/cni/test
echo "test" > /var/lib/cni/test/file
mkdir -p /etc/cni/net.d/test
echo "test" > /etc/cni/net.d/test/file

# Now uninstall with force - these directories should be removed
nodeadm uninstall --skip node-validation,pod-validation --force

assert::path-exists /var/lib/kubelet
assert::path-exists /var/lib/kubelet/test/file
assert::path-not-exist /etc/kubernetes/test/file
assert::path-not-exist /var/lib/cni/test/file
assert::path-not-exist /etc/cni/net.d/test/file
