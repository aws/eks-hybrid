#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
mock::kubelet 1.28.0
wait::dbus-ready

nodeadm init --skip run,install-validation,k8s-authentication-validation --config-source file://config.yaml

assert::files-equal /etc/containerd/config.toml expected-containerd-config.toml
assert::files-equal /etc/containerd/config.d/00-nodeadm.toml expected-user-containerd-config.toml
