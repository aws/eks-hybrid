// +kubebuilder:object:generate=true
// +groupName=node.eks.aws
// +kubebuilder:validation:Optional
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"

	"github.com/aws/eks-hybrid/api"
)

var (
	GroupVersion  = schema.GroupVersion{Group: api.GroupName, Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)
