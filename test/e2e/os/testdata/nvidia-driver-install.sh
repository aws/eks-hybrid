#!/bin/bash
set -e

# Detect if NVIDIA GPU is present
if ! lspci | grep -i nvidia > /dev/null; then
    echo "No NVIDIA GPU detected, skipping driver installation"
    exit 0
fi

echo "NVIDIA GPU detected, installing drivers"

# Detect architecture
ARCH=$(uname -m)
if [[ "$ARCH" == "x86_64" ]]; then
    ARCH_DIR_NVIDIA="x86_64"
    ARCH_NAME="amd64"
elif [[ "$ARCH" == "aarch64" ]]; then
    ARCH_DIR_NVIDIA="sbsa"
    ARCH_NAME="arm64"
else
    echo "Unsupported architecture: $ARCH"
    exit 1
fi

echo "Detected architecture: $ARCH_NAME"

# Detect OS type
OS_TYPE=""
PKG_TYPE=""
if [ -f /etc/os-release ]; then
    source /etc/os-release
    if [[ "$ID" == "amzn" ]]; then
        OS_TYPE="al23"
        PKG_TYPE="rpm"
    elif [[ "$ID" == "ubuntu" ]]; then
        OS_TYPE="ubuntu"
        PKG_TYPE="deb"
    elif [[ "$ID" == "rhel" ]]; then
        OS_TYPE="rhel"
        PKG_TYPE="rpm"
    fi
fi

if [ -z "$OS_TYPE" ]; then
    echo "Unsupported OS type, cannot install NVIDIA drivers"
    exit 1
fi

# Install drivers based on OS type
case $OS_TYPE in
    ubuntu)
        echo "Installing NVIDIA drivers for Ubuntu ($ARCH_NAME)"
        # https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/index.html#ubuntu-installation
        # Install required packages
        apt-get update
        apt-get install -y linux-headers-$(uname -r)

        # Determine Ubuntu version
        UBUNTU_VERSION="${VERSION_ID}"
        UBUNTU_VERSION_SHORT="${UBUNTU_VERSION/./}"

        if [[ "$UBUNTU_VERSION" == "22.04" ]]; then
            # Ubuntu 22.04 has an issue with gcc version 11 when build nvidia driver
            # Ref:https://forums.developer.nvidia.com/t/linux-new-kernel-6-5-0-14-ubuntu-22-04-can-not-compile-nvidia-display-card-driver/278553/9
            export NEEDRESTART_MODE=a
            export DEBIAN_FRONTEND=noninteractive
            apt install -y -qq gcc-12 gcc-11
            update-alternatives --install /usr/bin/gcc gcc /usr/bin/gcc-11 11
            update-alternatives --install /usr/bin/gcc gcc /usr/bin/gcc-12 12
            update-alternatives --set gcc /usr/bin/gcc-12
        fi

        # Add NVIDIA repository and install drivers
        wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu${UBUNTU_VERSION_SHORT}/${ARCH_DIR_NVIDIA}/cuda-keyring_1.1-1_all.deb
        dpkg -i cuda-keyring_1.1-1_all.deb
        apt-get update
        apt-get -y install nvidia-open
        ;;
    al23)
        echo "Installing NVIDIA drivers for Amazon Linux 2023 ($ARCH_NAME)"
        # https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/index.html#amazon-installation

        # Install required packages
        if [[ "$(uname -r)" == 6.1.* ]]; then
            dnf -y install kernel-modules-extra-$(uname -r) kernel-modules-extra-common-$(uname -r)
        fi

        dnf install -y kernel-devel-$(uname -r) kernel-headers-$(uname -r)

        # Add NVIDIA repository and install drivers
        dnf config-manager --add-repo https://developer.download.nvidia.com/compute/cuda/repos/amzn2023/${ARCH_DIR_NVIDIA}/cuda-amzn2023.repo
        dnf clean all
        dnf -y module enable nvidia-driver:open-dkms
        dnf install -y nvidia-open
        ;;
    rhel)
        echo "Installing NVIDIA drivers for RHEL ($ARCH_NAME)"
        # https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/index.html#rhel-installation
        RHEL_VERSION=""
        # Determine RHEL version and set up repository
        if [[ "$VERSION_ID" == "8"* ]]; then
            RHEL_VERSION="8"
        elif [[ "$VERSION_ID" == "9"* ]]; then
            RHEL_VERSION="9"
        else
            echo "Unsupported RHEL version"
            exit 1
        fi

        dnf install -y kernel-devel-$(uname -r) kernel-headers-$(uname -r)
        # This is to fix `Repositories disabled by configuration` error when enabling repos below
        subscription-manager config --rhsm.manage_repos=1

        subscription-manager repos --enable=rhel-${RHEL_VERSION}-for-$(uname -m)-appstream-rpms
        subscription-manager repos --enable=rhel-${RHEL_VERSION}-for-$(uname -m)-baseos-rpms
        subscription-manager repos --enable=codeready-builder-for-rhel-${RHEL_VERSION}-$(uname -m)-rpms
        dnf install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-${RHEL_VERSION}.noarch.rpm
        dnf config-manager --add-repo https://developer.download.nvidia.com/compute/cuda/repos/rhel${RHEL_VERSION}/${ARCH_DIR_NVIDIA}/cuda-rhel${RHEL_VERSION}.repo

        # Install NVIDIA drivers
        dnf clean all
        dnf -y module enable nvidia-driver:open-dkms
        dnf install -y nvidia-open
        ;;
esac

# Install NVIDIA Container Toolkit based on package type
echo "Installing NVIDIA Container Toolkit"
case $PKG_TYPE in
    deb)
        # Install NVIDIA Container Toolkit for Debian-based systems
        curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
        curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
            sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
            sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
        apt-get update
        apt-get install -y nvidia-container-toolkit
        ;;
    rpm)
        # Install NVIDIA Container Toolkit for RPM-based systems
        curl -s -L https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo | \
            sudo tee /etc/yum.repos.d/nvidia-container-toolkit.repo
        dnf install -y nvidia-container-toolkit
        ;;
esac

# Configure runtime and restart containerd
nvidia-ctk runtime configure --runtime=containerd
systemctl restart containerd

# Verify installation
if nvidia-smi > /dev/null; then
    echo "NVIDIA GPU drivers installed and verified successfully"
else
    echo "NVIDIA GPU driver installation failed"
    exit 1
fi
