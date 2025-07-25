#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /helpers.sh

mock::aws
mock::kubelet 1.28.0
wait::dbus-ready

nodeadm init --skip run,install-validation --config-source file://config.yaml

assert::file-contains /etc/eks/kubelet/environment '--v=5 --node-labels=foo=bar,foo2=baz --register-with-taints=foo=bar:NoSchedule"$'
