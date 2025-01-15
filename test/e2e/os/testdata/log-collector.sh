#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

LOGS_UPLOAD_URL="$1"

LOG_SCRIPT_URL="https://raw.githubusercontent.com/awslabs/amazon-eks-ami/refs/heads/main/log-collector-script/linux/eks-log-collector.sh"

for i in {1..5}; do curl --fail -s --retry 5 -L "$LOG_SCRIPT_URL" -o /tmp/eks-log-collector.sh && break || sleep 5; done

bash /tmp/eks-log-collector.sh --eks_hybrid=true

if ls /var/log/eks_* > /dev/null 2>&1; then
    curl --retry 5 --request PUT --upload-file /var/log/eks_* "${LOGS_UPLOAD_URL}"
    rm /var/log/eks_*
fi