#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

LOGS_UPLOAD_NAME="$1"
FAILBACK_UPLOAD_NAME="${2:-}"

declare -A LOGS_UPLOAD_URLS=()
{{ range $url := .LogsUploadUrls }}
LOGS_UPLOAD_URLS["{{ $url.Name }}"]="{{ $url.Url }}"
{{- end }}

if [ ! ${LOGS_UPLOAD_URLS[$LOGS_UPLOAD_NAME]+1} ]; then
    # no presigned url for name
    exit
fi

LOG_SCRIPT_URL="https://raw.githubusercontent.com/awslabs/amazon-eks-ami/refs/heads/main/log-collector-script/linux/eks-log-collector.sh"

curl -s --retry 5  $LOG_SCRIPT_URL -o /tmp/eks-log-collector.sh

bash /tmp/eks-log-collector.sh

if ls /var/log/eks_* > /dev/null 2>&1; then
    # do not overwrite if the file is already there
    # s3 will return a 412 PreconditionFailed if the file already exists
    HTTP_CODE=$(curl --header 'If-None-Match: *' -w "%{http_code}" -s -o /dev/null --retry 5 --request PUT --upload-file /var/log/eks_* "${LOGS_UPLOAD_URLS[$LOGS_UPLOAD_NAME]}")
    if [[ ${HTTP_CODE} -eq 412 ]] && [ -n "${FAILBACK_UPLOAD_NAME}" ] ; then
        curl --retry 5 --request PUT --upload-file /var/log/eks_* "${LOGS_UPLOAD_URLS[$FAILBACK_UPLOAD_NAME]}"
    fi
    rm /var/log/eks_*
fi