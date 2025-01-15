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

# https://docs.tigera.io/calico/latest/getting-started/kubernetes/quickstart
VERSION="$1"

curl -s --retry 5 -o tigera-operator.yaml https://raw.githubusercontent.com/projectcalico/calico/$VERSION/manifests/tigera-operator.yaml

# the calico-operator by default tolerations all taints
# this makes draining a difficult if the operator is running on that node
# since it will just immediately restart
# this restricts the toleration to the one needed during initialization
# more info: https://github.com/projectcalico/calico/issues/6136
yq -i '(select(.kind == "Deployment").spec.template.spec.tolerations[].key |= "node.kubernetes.io/not-ready")' tigera-operator.yaml

sed -i -e 's/quay.io/{{.ContainerRegistry}}/g' tigera-operator.yaml
echo "$VERSION" > VERSION
