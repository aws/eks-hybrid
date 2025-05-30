#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# Required arguments
PROD_BUCKET=$1
VERSION=$2
PUBLIC_READ_ACL="${3:-true}"

PUBLIC_READ_ACL_ARG=""  
if [ "$PUBLIC_READ_ACL" = "true" ]; then
  PUBLIC_READ_ACL_ARG="--acl public-read"
fi

echo "Starting nodeadm release process..."

# Upload to production
echo "Uploading artifacts to production..."
aws s3 sync --no-progress --exclude "*nodeadm.gz" latest/ s3://${PROD_BUCKET}/releases/${VERSION}/ ${PUBLIC_READ_ACL_ARG}
aws s3 sync --no-progress --include "*nodeadm.gz" --content-encoding gzip latest/ s3://${PROD_BUCKET}/releases/${VERSION}/ ${PUBLIC_READ_ACL_ARG}

# Update latest symlinks
echo "Updating latest symlinks for nodeadm..."
aws s3 sync --no-progress s3://${PROD_BUCKET}/releases/${VERSION}/ s3://${PROD_BUCKET}/releases/latest/

echo "Release process completed successfully"
