#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

ARTIFACTS_BUCKET="$1"

# only uploading nodeadm, ATTRIBUTION.txt, and GIT_VERSION, skipping ginkgo/e2e-test/nodeadm.test
mkdir -p _bin/latest/bin/linux/{amd64,arm64}
cp _bin/{ATTRIBUTION.txt,GIT_VERSION} _bin/latest/
cp _bin/amd64/nodeadm{,.gz,.sha256,.sha512,.gz.sha256,.gz.sha512} _bin/latest/bin/linux/amd64/
cp _bin/arm64/nodeadm{,.gz,.sha256,.sha512,.gz.sha256,.gz.sha512} _bin/latest/bin/linux/arm64/

# uploading nodeadm.gz files separately to ensure the content-encoding/disposition is applied
aws s3 sync --no-progress --exclude "*nodeadm.gz" _bin/latest/ s3://${ARTIFACTS_BUCKET}/latest/
aws s3 sync --no-progress --include "*nodeadm.gz" --content-encoding gzip --content-disposition "attachment; filename=\"nodeadm\"" _bin/latest/ s3://${ARTIFACTS_BUCKET}/latest/
