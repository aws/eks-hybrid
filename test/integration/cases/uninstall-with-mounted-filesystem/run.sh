#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Set USER environment variable to avoid Go runtime cgo issues
export USER=${USER:-root}

source /helpers.sh
source /test-constants.sh

mock::aws
wait::dbus-ready

# Function to create test files for uninstall validation
setup_test_files() {
  # Files and folders should not be deleted after uninstall
  mkdir -p /etc/
  echo "passwd" > /etc/passwd
  mkdir -p /var/lib/kubelet/test_pod/volumes
  mkdir -p /var/lib/kubelet/test_pod/volume-subpaths
  echo "test" > /var/lib/kubelet/test_pod/volume-subpaths/file 
  mkdir -p /var/lib/kubelet/test_pod/volume-subpaths/0
  
  # Create a test mount point outside kubelet directory
  mkdir -p /mnt/test-mount
  echo "test-mount-data" > /mnt/test-mount/data

  # Files/Folders that should be deleted without the force flag
  mkdir -p /etc/systemd/system
  echo "test" > /etc/systemd/system/kubelet.service
  echo "test" > /var/lib/kubelet/kubeconfig # kubeconfig file
  mkdir -p /etc/kubernetes/kubelet
  mkdir -p /etc/kubernetes/test
  echo "test" > /etc/kubernetes/test/file
  mkdir -p /etc/containerd
  mkdir -p /etc/eks/image-credential-provider
  echo "test" > /etc/eks/image-credential-provider/config.json

  # Files/Folders that should be deleted only with the force flag
  mkdir -p /var/lib/cni/test
  echo "test" > /var/lib/cni/test/file
  mkdir -p /etc/cni/net.d/test
  echo "test" > /etc/cni/net.d/test/file
}

# Remove previously installed containerd to test installation via nodeadm
dnf remove -y containerd

# Install a version to test uninstall
nodeadm install $CURRENT_VERSION --credential-provider ssm

# Create test files for first uninstall test
setup_test_files

# Create a bind mount to simulate a mounted filesystem that should be preserved
mock::bind-mount /mnt/test-mount /var/lib/kubelet/test_pod/volume-subpaths/0
assert::path-exists /var/lib/kubelet/test_pod/volume-subpaths/0/data

# First uninstall without force - these directories should remain
nodeadm uninstall --skip node-validation,pod-validation

# Verify that user data and mounted filesystems are preserved
assert::path-exists /var/lib/kubelet/test_pod/volumes
assert::path-exists /var/lib/kubelet/test_pod/volume-subpaths/file
assert::path-exists /var/lib/kubelet/test_pod/volume-subpaths/0/data

# Verify that system components are removed
assert::path-not-exist /usr/bin/kubelet
assert::path-not-exist /etc/systemd/system/kubelet.service
assert::path-not-exist /var/lib/kubelet/kubeconfig
assert::path-not-exist /etc/kubernetes/test/file
assert::path-not-exist /etc/containerd
assert::path-not-exist /etc/eks/image-credential-provider/config.json

# Verify that CNI files are preserved (not removed without force)
assert::path-exists /var/lib/cni/test/file
assert::path-exists /etc/cni/net.d/test/file

# Clean up the mount before reinstalling
mock::unbind-mount /var/lib/kubelet/test_pod/volume-subpaths/0
sleep 1
assert::path-not-exist /var/lib/kubelet/test_pod/volume-subpaths/0/data

# Install again to test force uninstall
nodeadm install $CURRENT_VERSION --credential-provider ssm

# Recreate test files for force uninstall test
setup_test_files

# Create the bind mount again
mock::bind-mount /mnt/test-mount /var/lib/kubelet/test_pod/volume-subpaths/0
assert::path-exists /var/lib/kubelet/test_pod/volume-subpaths/0/data

# Now uninstall with force - CNI directories should be removed but mounts preserved
nodeadm uninstall --skip node-validation,pod-validation --force

# Verify that user data and mounted filesystems are still preserved
assert::path-exists /var/lib/kubelet/test_pod/volumes
assert::path-exists /var/lib/kubelet/test_pod/volume-subpaths/file
assert::path-exists /var/lib/kubelet/test_pod/volume-subpaths/0/data

# Verify that system components are removed
assert::path-not-exist /usr/bin/kubelet
assert::path-not-exist /etc/systemd/system/kubelet.service
assert::path-not-exist /var/lib/kubelet/kubeconfig
assert::path-not-exist /etc/kubernetes/test/file
assert::path-not-exist /etc/containerd
assert::path-not-exist /etc/eks/image-credential-provider/config.json

# Verify that CNI files are removed with force
assert::path-not-exist /var/lib/cni/test/file
assert::path-not-exist /etc/cni/net.d/test/file

# Clean up the mount at the end (if it still exists)
mock::unbind-mount /var/lib/kubelet/test_pod/volume-subpaths/0
