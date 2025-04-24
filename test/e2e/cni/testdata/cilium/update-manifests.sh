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

CILIUM_VERSION=$1

OPERATOR_DIGEST=$(docker buildx imagetools inspect quay.io/cilium/operator-generic:$CILIUM_VERSION --format '{{json .Manifest.Digest}}')
CILIUM_DIGEST=$(docker buildx imagetools inspect quay.io/cilium/cilium:$CILIUM_VERSION --format '{{json .Manifest.Digest}}')

cat <<EOF > ./cilium-values.yaml
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: eks.amazonaws.com/compute-type
          operator: In
          values:
          - hybrid
operator:
  image:
    repository: "{{.ContainerRegistry}}/cilium/operator"
    tag: "$CILIUM_VERSION"
    imagePullPolicy: "IfNotPresent"
    digest: $OPERATOR_DIGEST
  replicas: 1
  unmanagedPodWatcher:
    restart: false
  # the cilium-operator by default tolerations all taints
  # this makes draining a difficult if the operator is running on that node
  # since it will just immediately restart
  # this restricts the toleration to the one needed during initialization
  # more info: https://github.com/cilium/cilium/pull/28856
  tolerations:
    - key: node.kubernetes.io/not-ready
      operator: Exists
    - key: node.cilium.io/agent-not-ready
      operator: Exists
ipam:
  mode: cluster-pool
envoy:
  enabled: false
image:
  repository: "{{.ContainerRegistry}}/cilium/cilium"
  tag: "$CILIUM_VERSION"
  imagePullPolicy: "IfNotPresent"
  digest: $CILIUM_DIGEST
preflight:
  image:
    repository: "{{.ContainerRegistry}}/cilium/cilium"
    tag: "$CILIUM_VERSION"
    imagePullPolicy: "IfNotPresent"
    digest: $CILIUM_DIGEST
# enable session affinity for conformance tests
sessionAffinity: true
EOF

helm repo add cilium https://helm.cilium.io/
helm repo update cilium
# the cilium chart generates different apparmor configuration depending on if the kube version is 1.29 and less vs 1.30 and above
helm template cilium cilium/cilium --version ${CILIUM_VERSION:1} --kube-version 1.29 --namespace kube-system --values ./cilium-values.yaml --set ipam.operator.clusterPoolIPv4PodCIDRList='\{\{.PodCIDR\}\}' >  ./cilium-template-129.yaml
helm template cilium cilium/cilium --version ${CILIUM_VERSION:1} --kube-version 1.30 --namespace kube-system --values ./cilium-values.yaml --set ipam.operator.clusterPoolIPv4PodCIDRList='\{\{.PodCIDR\}\}' >  ./cilium-template-130.yaml

echo "$CILIUM_VERSION" > VERSION
