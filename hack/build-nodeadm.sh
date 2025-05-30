#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

GIT_VERSION="$1"

# Generate attribution
echo "Generating attribution..."
make generate-attribution

echo "Building nodeadm and tests binaries..."
make build-cross-platform build-cross-e2e-tests-binary build-cross-e2e-test install-cross-ginkgo

echo "Compressing nodeadm binaries..."
for ARCH in amd64 arm64; do
    gzip --best < _bin/${ARCH}/nodeadm > _bin/${ARCH}/nodeadm.gz
done

# Generate and upload checksums
echo "Generating and uploading nodeadm checksums..."
for ARCH in amd64 arm64; do
  for FILE in nodeadm nodeadm.gz; do
    for CHECKSUM in sha256 sha512; do
      (cd _bin/${ARCH} && ${CHECKSUM}sum ${FILE} > ${FILE}.${CHECKSUM})
    done
  done
done

mv ATTRIBUTION.txt _bin/ATTRIBUTION.txt
echo $GIT_VERSION >> _bin/GIT_VERSION
