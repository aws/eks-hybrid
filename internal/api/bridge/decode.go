package bridge

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/api"
	internalapi "github.com/aws/eks-hybrid/internal/api"
)

// DecodeNodeConfig unmarshals the given data into an internal NodeConfig object.
// The data may be JSON or YAML.
func DecodeNodeConfig(data []byte) (*internalapi.NodeConfig, error) {
	scheme := runtime.NewScheme()
	err := localSchemeBuilder.AddToScheme(scheme)
	if err != nil {
		return nil, err
	}
	codecs := serializer.NewCodecFactory(scheme)
	obj, gvk, err := codecs.UniversalDecoder().Decode(data, nil, nil)
	if err != nil {
		return nil, err
	}
	if gvk.Kind != api.KindNodeConfig {
		return nil, fmt.Errorf("failed to decode %q (wrong Kind)", gvk.Kind)
	}
	if gvk.Group != api.GroupName {
		return nil, fmt.Errorf("failed to decode %q, unexpected group: %s", gvk.Kind, gvk.Group)
	}
	if internalConfig, ok := obj.(*internalapi.NodeConfig); ok {
		return internalConfig, nil
	}
	return nil, fmt.Errorf("unable to convert %T to internal NodeConfig", obj)
}

// DecodeStrictNodeConfig unmarshals the given data into an internal NodeConfig object.
// It attempts a struct unmarshalling. Will throw an error if unknown fields are present.
func DecodeStrictNodeConfig(data []byte) (*internalapi.NodeConfig, error) {
	var obj internalapi.NodeConfig
	if err := yaml.UnmarshalStrict(data, &obj); err != nil {
		return nil, err
	}

	return &obj, nil
}
