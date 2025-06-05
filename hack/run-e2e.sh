#!/usr/bin/env bash
# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

source $REPO_ROOT/hack/common.sh

CLUSTER_NAME="${1?Please specifiy the Cluster Name}"
REGION="${2?Please specify the AWS region}"
KUBERNETES_VERSION="${3?Please specify the Kubernetes version}"
CNI="${4?Please specify the cni}"
NODEADM_AMD_URL="${5?Please specify the nodeadm amd url}"
NODEADM_ARM_URL="${6?Please specify the nodeadm arm url}"
LOGS_BUCKET="${7-?Please specify the bucket for logs}"
ARTIFACTS_FOLDER="${8-?Please specify the folder for artifacts}"


CONFIG_DIR="$REPO_ROOT/e2e-config"
ARCH="$([ "x86_64" = "$(uname -m)" ] && echo amd64 || echo arm64)"
BIN_DIR="$REPO_ROOT/_bin/$ARCH"

SUITE_BIN="$BIN_DIR/${E2E_SUITE:-nodeadm.test}"
FILTER="${E2E_FILTER-(simpleflow) || (upgradeflow && (ubuntu2204-amd64 || rhel8-amd64 || al23-amd64))}"

mkdir -p $CONFIG_DIR

RESOURCES_YAML=$CONFIG_DIR/e2e-setup-spec.yaml
cat <<EOF > $RESOURCES_YAML
clusterName: $CLUSTER_NAME
clusterRegion: $REGION
eks:
  endpoint: "${EKS_ENDPOINT:-}"
  clusterRoleSP: "${EKS_CLUSTER_ROLE_SP:-}"
  podIdentitySP: "${EKS_POD_IDENTITY_SP:-}"
kubernetesVersion: $KUBERNETES_VERSION
cni: $CNI
clusterNetwork:
  vpcCidr: $CLUSTER_VPC_CIDR
  publicSubnetCidr: $CLUSTER_PUBLIC_SUBNET_CIDR
  privateSubnetCidr: $CLUSTER_PRIVATE_SUBNET_CIDR
hybridNetwork:
  vpcCidr: $HYBRID_VPC_CIDR
  publicSubnetCidr: $HYBRID_PUBLIC_SUBNET_CIDR
  privateSubnetCidr: $HYBRID_PRIVATE_SUBNET_CIDR
  podCidr: $HYBRID_POD_CIDR
EOF

SKIP_FILE=$REPO_ROOT/hack/SKIPPED_TESTS.yaml
# Extract skipped_tests field from SKIP_FILE file and join entries with ' || '
skip=$(yq '.skipped_tests | join("|")' ${SKIP_FILE})

build::common::echo_and_run $BIN_DIR/e2e-test run-e2e \
  --setup-config=$RESOURCES_YAML \
  --test-filter="$FILTER" \
  --tests-binary=$SUITE_BIN \
  --skipped-tests="$skip" \
  --nodeadm-amd-url=$NODEADM_AMD_URL \
  --nodeadm-arm-url=$NODEADM_ARM_URL \
  --logs-bucket=$LOGS_BUCKET \
  --artifacts-dir=$ARTIFACTS_FOLDER \
  --procs=64 \
  --skip-cleanup=false \
  --no-color
