FROM public.ecr.aws/eks-distro-build-tooling/golang:1.25 AS nodeadm-build
WORKDIR /go/src/github.com/aws/eks-hybrid
ARG GOPROXY
RUN go env -w GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build
RUN mv _bin/nodeadm /nodeadm

# Pin AL2023 base image digest for reproducible builds
# To update: docker pull public.ecr.aws/amazonlinux/amazonlinux:2023 && docker inspect --format '{{index .RepoDigests 0}}'
FROM public.ecr.aws/amazonlinux/amazonlinux@sha256:a173dcd0fdeb0e83b39fcf9464a1b2af233569809181717cf541ab6a82039c09
ARG TARGETARCH

# Don't run dnf update — pinned base image provides reproducibility.
# Update the image digest above when intentionally upgrading.
RUN dnf -y install systemd containerd jq git-core python3.11 python3.11-pip tar procps zip openssl openssl-devel && \
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

# Pin moto version for reproducibility. Python 3.11 required — AL2023's Python 3.9.25
# has a pkgutil.get_data security patch (CVE-2026-11972, July 2026) that breaks moto's
# resource loading on all moto versions.
RUN python3.11 -m pip install --user 'moto[server]==5.2.2'


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
