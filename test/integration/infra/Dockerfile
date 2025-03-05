FROM public.ecr.aws/eks-distro-build-tooling/golang:1.23 AS imds-mock-build
ARG TARGETARCH
RUN curl -Lo /imds-mock https://github.com/aws/amazon-ec2-metadata-mock/releases/download/v1.12.0/ec2-metadata-mock-linux-${TARGETARCH}
RUN chmod +x /imds-mock

FROM public.ecr.aws/eks-distro-build-tooling/golang:1.23 AS nodeadm-build
WORKDIR /go/src/github.com/aws/eks-hybrid
ARG GOPROXY
RUN go env -w GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build
RUN mv _bin/nodeadm /nodeadm

FROM public.ecr.aws/amazonlinux/amazonlinux:2023
RUN dnf -y update && \
    dnf -y install systemd containerd jq python3 awscli tar procps && \
    dnf clean all
RUN curl -O https://bootstrap.pypa.io/get-pip.py && \
    python3 get-pip.py && \
    rm get-pip.py
RUN pip install --user 'moto[server]'
# I know how this looks, but it lets us use moto with our mocked IMDS and for now the simplicity is worth the hack
RUN sed -i 's/= random_instance_id()/= "i-1234567890abcdef0"/g' $HOME/.local/lib/python*/site-packages/moto/ec2/models/instances.py
COPY --from=imds-mock-build /imds-mock /usr/local/bin/imds-mock
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

RUN mkdir -p /etc/eks/image-credential-provider/
RUN touch /etc/eks/image-credential-provider/ecr-credential-provider
ENV CPU_DIR /sys_devices_system_mock/cpu
ENV NODE_DIR /sys_devices_system_mock/node

# GPG is invoked by ssm for signature verification under few test scenarios
# Without tty inside the container, gpg commands fail. With this config gpg ignores tty
RUN mkdir -p $HOME/.gnupg
RUN touch $HOME/.gnupg/gpg.conf
RUN echo "no-tty" >> $HOME/.gnupg/gpg.conf

ENTRYPOINT ["/usr/lib/systemd/systemd","--system"]
