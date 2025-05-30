#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

ARTIFACTS_BUCKET="$1"
PUBLIC_READ_ACL="${2:-true}"

PUBLIC_READ_ACL_ARG=""  
if [ "$PUBLIC_READ_ACL" = "true" ]; then
  PUBLIC_READ_ACL_ARG="--acl public-read"
fi

# only uploading nodeadm, ATTRIBUTION.txt, and GIT_VERSION, skipping ginkgo/e2e-test/nodeadm.test
mkdir -p _bin/latest/linux/{amd64,arm64}
cp _bin/{ATTRIBUTION.txt,GIT_VERSION} _bin/latest/
cp _bin/amd64/nodeadm{,.gz,.sha256,.sha512,.gz.sha256,.gz.sha512} _bin/latest/linux/amd64/
cp _bin/arm64/nodeadm{,.gz,.sha256,.sha512,.gz.sha256,.gz.sha512} _bin/latest/linux/arm64/

# uploading nodeadm.gz files separately to ensure the content-encoding is applied
aws s3 sync --no-progress --exclude "*nodeadm.gz" _bin/latest/ s3://${ARTIFACTS_BUCKET}/latest/ ${PUBLIC_READ_ACL_ARG}
aws s3 sync --no-progress --include "*nodeadm.gz" --content-encoding gzip _bin/latest/ s3://${ARTIFACTS_BUCKET}/latest/ ${PUBLIC_READ_ACL_ARG}
