#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /etc/proxy-vars.sh

if command -v apt-get >/dev/null 2>&1; then
    # we are installing the key and setting up the docker repo here
    # instead in the cloud-init template because (i think) cloud-init is using
    # apt-key which does not seem to be proxy aware and/or the proxy config set
    # in the cloud-init template is not taking affect before it attempts to
    # install the key and setup the repo
    # https://docs.docker.com/engine/install/ubuntu/
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc

    echo \
        "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
        $(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}") stable" | \
        tee /etc/apt/sources.list.d/docker.list > /dev/null

    CMD="apt-get -o DPkg::Lock::Timeout=60"
    UPDATE_SUBCMD="update"
    LOCK_FILE="/var/lib/dpkg/lock-frontend"
    PACKAGE="containerd.io=1.*"
else
    CMD="yum"
    UPDATE_SUBCMD="makecache"
    LOCK_FILE="/var/lib/rpm/.rpm.lock"
    PACKAGE="containerd.io-1.*"
fi

for subcmd in $UPDATE_SUBCMD "install -y $PACKAGE"; do
    ATTEMPTS=5
    while ! $CMD $subcmd ; do
        echo "$CMD failed to $subcmd"
    
        # attempt to wait for any in progress apt-get/yum operations to complete
        while find /proc/*/fd -ls | grep $LOCK_FILE >/dev/null 2>&1; do
            echo "waiting for process with lock on $LOCK_FILE to complete"
            sleep 1
        done

        ((ATTEMPTS--)) || break
        sleep 5
    done
done

if ! command -v ctr >/dev/null 2>&1; then 
    echo "containerd failed to installed"
    exit 1
fi
