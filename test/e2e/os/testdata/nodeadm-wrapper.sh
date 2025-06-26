#!/usr/bin/env bash
source /etc/proxy-vars.sh
AWS_ENDPOINT_URL_EKS={{ .EKSEndpoint }} /usr/local/bin/nodeadm "$@"