#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

declare SUPPORTED_VERSIONS=(1.27 1.28 1.29 1.30 1.31)

for VERSION in ${SUPPORTED_VERSIONS}
do
  if nodeadm install $VERSION --credential-provider ssm --download-timeout 1s; then
    echo "install should not succeed in 1 second"
    exit 1
  fi
done
