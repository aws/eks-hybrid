#!/bin/bash
set -eux

sudo apt-get update -y
sudo apt-get install -y curl cloud-init

sudo curl "$nodeadm_link" -o /usr/local/bin/nodeadm
sudo chmod +x /usr/local/bin/nodeadm
sudo /usr/local/bin/nodeadm install "$k8s_version" --credential-provider "$auth_value"

sudo cloud-init clean
