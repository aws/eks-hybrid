FROM public.ecr.aws/eks-distro-build-tooling/golang:1.24 AS nodeadm-build
WORKDIR /go/src/github.com/aws/eks-hybrid
ARG GOPROXY
RUN go env -w GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build
RUN mv _bin/nodeadm /nodeadm

FROM public.ecr.aws/amazonlinux/amazonlinux:2023
ARG TARGETARCH

RUN dnf -y update && \
    dnf -y install systemd containerd jq git-core python3 tar procps zip openssl openssl-devel && \
    dnf clean all

RUN curl -OL https://github.com/mikefarah/yq/releases/download/v4.45.3/yq_linux_${TARGETARCH}.tar.gz && \
    tar -C /usr/bin -xzf yq_linux_${TARGETARCH}.tar.gz && \
    mv /usr/bin/yq_linux_${TARGETARCH} /usr/bin/yq

RUN curl -Lo /imds-mock https://github.com/aws/amazon-ec2-metadata-mock/releases/download/v1.13.0/ec2-metadata-mock-linux-${TARGETARCH} && \
    chmod +x /imds-mock && \
    cp /imds-mock /usr/local/bin/imds-mock

RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-$(uname -m).zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf aws awscliv2.zip

RUN curl -O https://bootstrap.pypa.io/get-pip.py && \
    python3 get-pip.py && \
    rm get-pip.py

RUN pip install --user 'moto[server]'


# I know how this looks, but it lets us use moto with our mocked IMDS and for now the simplicity is worth the hack
RUN sed -i 's/= random_instance_id()/= "i-1234567890abcdef0"/g' $HOME/.local/lib/python*/site-packages/moto/ec2/models/instances.py
# The content of ec2 userdata in the 'aemm-default-config.json'
# file is the base64 encoding of a minimally viable NodeConfig.
# At the time of this change, it is equal to the following:
#
# ---
# apiVersion: node.eks.aws/v1alpha1
# kind: NodeConfig
# spec:
#   cluster:
#     name: my-cluster
#     apiServerEndpoint: https://example.com
#     certificateAuthority: Y2VydGlmaWNhdGVBdXRob3JpdHk=
#     cidr: 10.100.0.0/16
COPY test/integration/infra/aemm-default-config.json /etc/aemm-default-config.json
COPY --from=nodeadm-build /nodeadm /usr/local/bin/nodeadm
COPY test/integration/infra/systemd/kubelet.service /usr/lib/systemd/system/kubelet.service
COPY test/integration/infra/systemd/containerd.service /usr/lib/systemd/system/containerd.service
COPY test/integration/infra/mock/ /sys_devices_system_mock/
COPY test/integration/helpers.sh /helpers.sh
COPY test/integration/test-constants.sh /test-constants.sh

RUN mkdir -p /etc/eks/image-credential-provider/
RUN touch /etc/eks/image-credential-provider/ecr-credential-provider
ENV CPU_DIR /sys_devices_system_mock/cpu
ENV NODE_DIR /sys_devices_system_mock/node

ENTRYPOINT ["/usr/lib/systemd/systemd","--system"]
