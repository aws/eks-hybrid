#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

ARTIFACTS_BUCKET="$1"
BUCKET_PREFIX="$2"

# *********************************************************************
# DO NOT EDIT this list unless you are sure you know what you are doing
# *********************************************************************

# expected list of files is intentional hardcoded and not generated
# to ensure we do not accidentally upload new files or remove files from the list  
# this will run after staging and prod releases
EXPECTED_FILES_FILE=$(mktemp)
cat <<EOF > ${EXPECTED_FILES_FILE}
ATTRIBUTION.txt
GIT_VERSION
bin/linux/amd64/nodeadm
bin/linux/amd64/nodeadm.gz
bin/linux/amd64/nodeadm.gz.sha256
bin/linux/amd64/nodeadm.gz.sha512
bin/linux/amd64/nodeadm.sha256
bin/linux/amd64/nodeadm.sha512
bin/linux/arm64/nodeadm
bin/linux/arm64/nodeadm.gz
bin/linux/arm64/nodeadm.gz.sha256
bin/linux/arm64/nodeadm.gz.sha512
bin/linux/arm64/nodeadm.sha256
bin/linux/arm64/nodeadm.sha512
EOF
# *********************************************************************

echo "Validating release artifacts..."

# get a list of files via s3 cli
if ! S3_FILES=$(aws s3 ls s3://${ARTIFACTS_BUCKET}/${BUCKET_PREFIX}/ --recursive | awk '{print $4}' | sed -e "s#^${BUCKET_PREFIX}/##" | grep -Pv '^$|.*/$'); then
    echo "Failed to get list of files from S3"
    exit 1
fi

S3_FILES_FILE=$(mktemp)
sort <(echo "${S3_FILES[@]}") > ${S3_FILES_FILE}

if ! diff -q ${EXPECTED_FILES_FILE} ${S3_FILES_FILE} &>/dev/null; then
    echo "Artifacts directory on S3 does not matched expected!"
    diff -y ${EXPECTED_FILES_FILE} ${S3_FILES_FILE}
    exit 1
fi

echo "Release artifacts validated successfully"
