#!/usr/bin/env bash

# Build nodeadm command with optional manifest override
NODEADM_ARGS=("$@")

{{- if .ManifestURL }}
# Add --manifest-override flag only for commands that support it
COMMAND="${1:-}"
if [[ "$COMMAND" == "init" || "$COMMAND" == "upgrade" || "$COMMAND" == "install" ]]; then
  NODEADM_ARGS=("--manifest-override" "{{ .ManifestURL }}" "${NODEADM_ARGS[@]}")
fi
{{- end }}
{{- if .PrivateMode }}
# Add --private-mode flag only for commands that support it
COMMAND="${1:-}"
if [[ "$COMMAND" == "init" || "$COMMAND" == "upgrade" || "$COMMAND" == "install" ]]; then
  # Append after the subcommand arguments so the bool flag can't consume the subcommand name
  NODEADM_ARGS=("${NODEADM_ARGS[@]}" "--private-mode")
fi
{{- end }}

AWS_ENDPOINT_URL_EKS={{ .EKSEndpoint }} /usr/local/bin/nodeadm "${NODEADM_ARGS[@]}"
