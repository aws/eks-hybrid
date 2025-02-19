FROM public.ecr.aws/bottlerocket/bottlerocket-admin:v0.11.16 as br-admin

FROM public.ecr.aws/amazonlinux/amazonlinux:2023

RUN yum install -y awscli jq sudo util-linux-core wget

RUN wget -qO /usr/local/bin/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_$([ "x86_64" = "$(uname -m)" ] && echo amd64 || echo arm64) && chmod +x /usr/local/bin/yq

# iam-auth exec's aws_signing_helper via the aws sdk which execs it thru sh
# https://github.com/aws/aws-sdk-go-v2/blob/061fd1b6d9940c6b37974e6aa64a5f9dc4ee1dc4/credentials/processcreds/provider.go#L93
COPY --from=br-admin /bin/bash /static-sh

COPY start-br-setup.sh /

ENTRYPOINT [ "/start-br-setup.sh" ]
