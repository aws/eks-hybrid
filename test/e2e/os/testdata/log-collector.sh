#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source /etc/proxy-vars.sh

# first arg is the url to upload the logs to
LOGS_UPLOAD_URL="$1"
# remaining args are the additional folders/files to add to the bundle
shift

LOG_SCRIPT_URL="https://raw.githubusercontent.com/awslabs/amazon-eks-ami/refs/heads/main/log-collector-script/linux/eks-log-collector.sh"

for i in {1..5}; do curl --fail -s --retry 5 -L "$LOG_SCRIPT_URL" -o /tmp/eks-log-collector.sh && break || sleep 5; done

bash /tmp/eks-log-collector.sh --eks_hybrid=true

if ls /var/log/eks_* > /dev/null 2>&1; then
    # this will find the latest bundle created by the log collector script
    file=$(ls -t /var/log/eks_* | head -1)
    if [ "$#" -gt 0 ]; then
        # add additional folders/files to generated bundle
        TMP_EXTRACTED_BUNDLE=$(mktemp -d)
        ADDITIONAL_LOGS_DIR="$TMP_EXTRACTED_BUNDLE/e2e-additional-logs"
        mkdir -p "$ADDITIONAL_LOGS_DIR"
        tar -xf "$file" -C "$TMP_EXTRACTED_BUNDLE"
        for arg in "$@"; do
            if [ ! -f "$arg" ] && [ ! -d "$arg" ]; then
                continue
            fi
            mkdir -p "$(dirname "$ADDITIONAL_LOGS_DIR/$arg")"
            cp -rf "$arg" "$ADDITIONAL_LOGS_DIR/$arg"
        done
        tar -C "$TMP_EXTRACTED_BUNDLE" -czf "$file" .  > /dev/null 2>&1
        rm -rf "$TMP_EXTRACTED_BUNDLE"
    fi
    curl --retry 5 --request PUT --upload-file "$file" "${LOGS_UPLOAD_URL}"
    rm /var/log/eks_*
fi
