package bridge

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/aws/eks-hybrid/api"
	"github.com/aws/eks-hybrid/api/v1alpha1"
	internalapi "github.com/aws/eks-hybrid/internal/api"
)

var localSchemeBuilder = runtime.NewSchemeBuilder(
	v1alpha1.AddToScheme,
	addInternalTypes,
)

func addInternalTypes(scheme *runtime.Scheme) error {
	groupVersion := schema.GroupVersion{Group: api.GroupName, Version: runtime.APIVersionInternal}
	scheme.AddKnownTypes(groupVersion,
		&internalapi.NodeConfig{},
	)
	return nil
}
