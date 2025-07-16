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

# Install a version to test uninstall functionality
nodeadm install $CURRENT_VERSION --credential-provider ssm

# Create test directories and files that would normally be cleaned up
mkdir -p /var/lib/kubelet/test-data
mkdir -p /var/lib/cni/test-data
mkdir -p /etc/kubernetes/test-data

# Create some test files
echo "test-kubelet-data" > /var/lib/kubelet/test-data/file
echo "test-cni-data" > /var/lib/cni/test-data/file
echo "test-k8s-data" > /etc/kubernetes/test-data/file

echo "🧪 Testing normal uninstall behavior..."
echo "📁 Files before uninstall:"
ls -la /var/lib/kubelet/test-data/ || echo "kubelet test-data not found"
ls -la /var/lib/cni/test-data/ || echo "cni test-data not found"
ls -la /etc/kubernetes/test-data/ || echo "kubernetes test-data not found"

# Test 1: Normal uninstall - should work with current SafeRemoveAll(dst, false, false)
echo "🧪 Testing normal uninstall..."
nodeadm uninstall --skip node-validation,pod-validation

echo "📁 Files after normal uninstall:"
ls -la /var/lib/kubelet/test-data/ 2>/dev/null || echo "✅ kubelet test-data removed"
ls -la /var/lib/cni/test-data/ 2>/dev/null || echo "✅ cni test-data removed"  
ls -la /etc/kubernetes/test-data/ 2>/dev/null || echo "✅ kubernetes test-data removed"

# Test 2: Install again and test force uninstall
echo "🧪 Installing again for force uninstall test..."
nodeadm install $CURRENT_VERSION --credential-provider ssm

# Create test data again
mkdir -p /var/lib/kubelet/test-force
mkdir -p /var/lib/cni/test-force
mkdir -p /etc/kubernetes/test-force

echo "test-kubelet-force" > /var/lib/kubelet/test-force/file
echo "test-cni-force" > /var/lib/cni/test-force/file
echo "test-k8s-force" > /etc/kubernetes/test-force/file

echo "🧪 Testing force uninstall..."
nodeadm uninstall --skip node-validation,pod-validation --force

echo "📁 Files after force uninstall:"
ls -la /var/lib/kubelet/test-force/ 2>/dev/null || echo "✅ kubelet test-force removed"
ls -la /var/lib/cni/test-force/ 2>/dev/null || echo "✅ cni test-force removed"
ls -la /etc/kubernetes/test-force/ 2>/dev/null || echo "✅ kubernetes test-force removed"

# Verify key directories are cleaned up as expected
assert::path-not-exist /var/lib/kubelet/test-force/file
assert::path-not-exist /var/lib/cni/test-force/file
assert::path-not-exist /etc/kubernetes/test-force/file

echo "✅ SafeRemoveAll integration test completed successfully"
echo "📝 Note: This test validates current behavior with SafeRemoveAll(dst, false, false)"
echo "📝 When you extend to use allowUnmount/forceUnmount parameters, this test"
echo "📝 can be enhanced to test actual mount point handling."