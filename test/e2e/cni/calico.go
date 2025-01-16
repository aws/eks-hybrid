package cni

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"

	"k8s.io/client-go/dynamic"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

//go:embed testdata/calico/tigera-operator.yaml
var tigeraTemplate []byte

//go:embed testdata/calico/calico-template.yaml
var calicoTemplate []byte

type Calico struct {
	K8s dynamic.Interface
	// podCIDR is the cluster level CIDR to be use for Pods. It needs to be big enough for
	// Hybrid Nodes.
	//
	// Check the calico-template file for the node pod cidr mask. The default is 24.
	podCIDR string
	region  string
}

func NewCalico(k8s dynamic.Interface, podCIDR, region string) Calico {
	return Calico{
		K8s:     k8s,
		podCIDR: podCIDR,
		region:  region,
	}
}

// Deploy creates or updates the Calico reosurces.
func (c Calico) Deploy(ctx context.Context) error {
	tmpl, err := template.New("calico").Parse(string(calicoTemplate))
	if err != nil {
		return err
	}
	values := map[string]string{
		"PodCIDR":           c.podCIDR,
		"ContainerRegistry": constants.EcrAccounId + ".dkr.ecr." + c.region + ".amazonaws.com/quay.io",
	}
	installation := &bytes.Buffer{}
	err = tmpl.Execute(installation, values)
	if err != nil {
		return err
	}

	tmpl, err = template.New("tigera").Parse(string(tigeraTemplate))
	if err != nil {
		return err
	}
	tigera := &bytes.Buffer{}
	err = tmpl.Execute(tigera, values)
	if err != nil {
		return err
	}

	objs, err := yamlToUnstructured(append(tigera.Bytes(), installation.Bytes()...))
	if err != nil {
		return err
	}

	fmt.Println("Applying calico installation")

	return upsertManifestsWithRetries(ctx, c.K8s, objs)
}
