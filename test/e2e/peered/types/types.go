package types

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	clientgo "k8s.io/client-go/kubernetes"
)

var (
	_ clientgo.Interface = K8s{}
	_ dynamic.Interface  = K8s{}
)

type K8s struct {
	clientgo.Interface
	Dynamic dynamic.Interface
}

func (k K8s) Resource(resource schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return k.Dynamic.Resource(resource)
}
