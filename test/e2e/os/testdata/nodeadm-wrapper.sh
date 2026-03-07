#!/usr/bin/env bash

# Build nodeadm command with optional manifest override
NODEADM_ARGS=("$@")

{{- if .ManifestURL }}
# Add --manifest-override flag if ManifestURL is provided
NODEADM_ARGS=("--manifest-override" "{{ .ManifestURL }}" "${NODEADM_ARGS[@]}")
{{- end }}

AWS_ENDPOINT_URL_EKS={{ .EKSEndpoint }} /usr/local/bin/nodeadm "${NODEADM_ARGS[@]}"
