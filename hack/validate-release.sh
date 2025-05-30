#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# Required arguments
CLOUDFRONT_ID=$1
VERSION_FILE=$2

echo "Starting release validation..."

# Create and wait for CloudFront invalidation
echo "Invalidating CloudFront cache..."
INVALIDATION_ID=$(aws cloudfront create-invalidation --distribution-id "${CLOUDFRONT_ID}" --paths "/releases/latest/bin/*" --query 'Invalidation.Id' --output text)
echo "Created invalidation with ID: ${INVALIDATION_ID}"

echo "Waiting for CloudFront invalidation to complete..."
while true; do
    STATUS=$(aws cloudfront get-invalidation --distribution-id "${CLOUDFRONT_ID}" --id "${INVALIDATION_ID}" --query 'Invalidation.Status' --output text)
    echo "Current invalidation status: ${STATUS}"
    if [ "${STATUS}" = "Completed" ]; then
        break
    elif [ "${STATUS}" = "Failed" ]; then
        echo "CloudFront invalidation failed!"
        exit 1
    fi
    sleep 10
done
echo "CloudFront invalidation completed successfully"

# Validate released version
echo "Validating released version..."
curl -L -o released_nodeadm https://hybrid-assets.eks.amazonaws.com/releases/latest/bin/linux/amd64/nodeadm
chmod +x released_nodeadm

# Extract just the semantic version using regex i.e. 'Version: v1.0.5' -> 'v1.0.5'
NODEADM_VERSION_OUTPUT=$(./released_nodeadm version)
RELEASED_VERSION=$(echo "${NODEADM_VERSION_OUTPUT}" | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+\(-[0-9a-zA-Z.-]\+\)\?' || echo "VERSION_NOT_FOUND")
EXPECTED_VERSION=$(cat "${VERSION_FILE}")

if [ "${RELEASED_VERSION}" != "${EXPECTED_VERSION}" ]; then
    echo "Version mismatch! Released version (${RELEASED_VERSION}) does not match expected version (${EXPECTED_VERSION})"
    exit 1
fi
echo "Version validation successful"

echo "Production release completed successfully"
echo "Version: $(cat ${VERSION_FILE})"
