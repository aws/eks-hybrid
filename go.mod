module github.com/aws/eks-hybrid

go 1.25.0

require (
	github.com/ProtonMail/gopenpgp/v3 v3.3.0
	github.com/aws/aws-sdk-go-v2/config v1.31.20
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.13
	github.com/aws/aws-sdk-go-v2/service/acmpca v1.46.2
	github.com/aws/aws-sdk-go-v2/service/cloudformation v1.69.0
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.58.9
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.270.0
	github.com/aws/aws-sdk-go-v2/service/ec2instanceconnect v1.32.11
	github.com/aws/aws-sdk-go-v2/service/ecr v1.52.0
	github.com/aws/aws-sdk-go-v2/service/ecrpublic v1.38.4
	github.com/aws/aws-sdk-go-v2/service/eks v1.74.9
	github.com/aws/aws-sdk-go-v2/service/iam v1.50.2
	github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi v1.30.13
	github.com/aws/aws-sdk-go-v2/service/rolesanywhere v1.21.12
	github.com/aws/aws-sdk-go-v2/service/s3 v1.90.2
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.39.13
	github.com/aws/aws-sdk-go-v2/service/ssm v1.67.2
	github.com/aws/aws-sdk-go-v2/service/sts v1.40.2
	github.com/aws/smithy-go v1.23.2
	github.com/cert-manager/aws-privateca-issuer v1.7.1
	github.com/cert-manager/cert-manager v1.19.1
	github.com/containerd/containerd v1.7.29
	github.com/coreos/go-systemd/v22 v22.6.0
	github.com/go-ini/ini v1.67.0
	github.com/go-logr/zapr v1.3.0
	github.com/integrii/flaggy v1.8.0
	github.com/onsi/ginkgo/v2 v2.27.2
	github.com/onsi/gomega v1.38.2
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.11.1
	github.com/tredoe/osutil v1.5.0
	go.uber.org/zap v1.27.0
	golang.org/x/crypto v0.43.0
	golang.org/x/mod v0.30.0
	k8s.io/apimachinery v0.34.2
	k8s.io/client-go v0.34.2
	k8s.io/cri-api v0.34.2
	k8s.io/kubectl v0.34.2
	k8s.io/kubelet v0.34.2
	sigs.k8s.io/controller-runtime v0.22.3
	sigs.k8s.io/hydrophone v0.7.0
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/ProtonMail/go-crypto v1.3.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.13 // indirect
	github.com/chai2010/gettext-go v1.0.3 // indirect
	github.com/cloudflare/circl v1.6.1 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-openapi/swag/jsonname v0.25.1 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/lmittmann/tint v1.0.4 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.opentelemetry.io/otel v1.37.0 // indirect
	go.opentelemetry.io/otel/trace v1.37.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	k8s.io/apiextensions-apiserver v0.34.1 // indirect
	k8s.io/cli-runtime v0.34.2 // indirect
	sigs.k8s.io/gateway-api v1.4.0 // indirect
	sigs.k8s.io/kustomize/api v0.20.1 // indirect
	sigs.k8s.io/kustomize/kyaml v0.20.1 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
)

require (
	dario.cat/mergo v1.0.2 // direct
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/aws/aws-sdk-go-v2 v1.39.6
	github.com/aws/aws-sdk-go-v2/credentials v1.18.24
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.13 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/route53 v1.58.4
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.7 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/go-logr/logr v1.4.3
	github.com/go-openapi/jsonpointer v0.22.1 // indirect
	github.com/go-openapi/jsonreference v0.21.2 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20250820193118-f64d9cf942d6 // indirect
	github.com/google/uuid v1.6.0
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/spf13/cobra v1.10.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.46.0
	golang.org/x/oauth2 v0.31.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/term v0.36.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	golang.org/x/time v0.13.0 // indirect
	golang.org/x/tools v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250929231259-57b25ae835d4 // indirect
	google.golang.org/grpc v1.75.1 // indirect
	google.golang.org/protobuf v1.36.9 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/api v0.34.2
	k8s.io/component-base v0.34.2 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250910181357-589584f1c912 // indirect
	k8s.io/metrics v0.34.2
	k8s.io/utils v0.0.0-20250820121507-0af2bda4dd1d // direct
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/yaml v1.6.0
)
