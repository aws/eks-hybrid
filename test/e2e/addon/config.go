package addon

import (
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"

	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

// AddonTestConfig contains common configuration fields shared across addon tests.
// This reduces duplication and makes it easier to add new common fields in the future.
type AddonTestConfig struct {
	Cluster    string
	K8S        peeredtypes.K8s
	EKSClient  *eks.Client
	K8SConfig  *rest.Config
	Logger     logr.Logger
	Region     string
	EcrAccount string
	DNSSuffix  string
}
