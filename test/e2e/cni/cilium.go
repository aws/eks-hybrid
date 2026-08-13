package cni

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/dynamic"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/images"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

//go:embed testdata/cilium/VERSION
var ciliumVersionFile string

// ciliumVersion is the Cilium version used for all regions, read from testdata/cilium/VERSION.
var ciliumVersion = strings.TrimSpace(ciliumVersionFile)

// For kubernetes versions less than 1.30, the cilium template uses
// annonations to add AppArmor configuration
//
//go:embed testdata/cilium/cilium-template-129.yaml
var ciliumTemplate129 []byte

// For kubernetes versions 1.30 and above, the AppArmor configuration
// is in spec.securityContext which is only available in 1.30+
//
//go:embed testdata/cilium/cilium-template-130.yaml
var ciliumTemplate130 []byte

type Cilium struct {
	k8s               dynamic.Interface
	kubernetesVersion string
	// podCIDR is the cluster level CIDR to be use for Pods. It needs to be big enough for
	// Hybrid Nodes.
	//
	// Check the cilium-template file for the node pod cidr mask. The default is 24.
	podCIDR string
	region  string
}

func NewCilium(k8s dynamic.Interface, podCIDR, region, kubernetesVersion string) Cilium {
	return Cilium{
		k8s:               k8s,
		kubernetesVersion: kubernetesVersion,
		podCIDR:           podCIDR,
		region:            region,
	}
}

// getCiliumImageConfig returns the appropriate image repository and tag based on region.
// For China regions, it uses the China ECR registry (constants.ChinaCiliumEcrAccountId).
// The same Cilium version (read from testdata/cilium/VERSION) is used for all regions.
func getCiliumImageConfig(region string) (ciliumRepo, operatorRepo, tag string) {
	if images.IsChinaRegion(region) {
		baseRepo := constants.ChinaCiliumEcrAccountId + ".dkr.ecr." + region + ".amazonaws.com.cn"
		return baseRepo + "/cilium/cilium",
			baseRepo + "/cilium/operator-generic",
			ciliumVersion
	}
	// Use public ECR for all other regions
	return constants.CiliumPublicEcrRegistry + "/cilium/cilium",
		constants.CiliumPublicEcrRegistry + "/cilium/operator-generic",
		ciliumVersion
}

// Deploy creates or updates the Cilium reosurces.
func (c Cilium) Deploy(ctx context.Context) error {
	ciliumTemplate, err := ciliumTemplate(c.kubernetesVersion)
	if err != nil {
		return err
	}
	tmpl, err := template.New("cilium").Parse(string(ciliumTemplate))
	if err != nil {
		return err
	}

	ciliumRepo, operatorRepo, tag := getCiliumImageConfig(c.region)

	values := map[string]string{
		"PodCIDR":       c.podCIDR,
		"CiliumImage":   ciliumRepo + ":" + tag,
		"OperatorImage": operatorRepo + ":" + tag,
	}
	installation := &bytes.Buffer{}
	err = tmpl.Execute(installation, values)
	if err != nil {
		return err
	}

	objs, err := kubernetes.YamlToUnstructured(installation.Bytes())
	if err != nil {
		return err
	}

	fmt.Printf("Applying cilium installation (region: %s, cilium image: %s, operator image: %s)\n",
		c.region, values["CiliumImage"], values["OperatorImage"])

	return kubernetes.UpsertManifestsWithRetries(ctx, c.k8s, objs)
}

func ciliumTemplate(kubernetesVersion string) ([]byte, error) {
	kubeVersion, err := version.ParseSemantic(kubernetesVersion + ".0")
	if err != nil {
		return nil, fmt.Errorf("parsing version: %v", err)
	}
	if kubeVersion.LessThan(version.MustParseSemantic("1.30.0")) {
		return ciliumTemplate129, nil
	}
	return ciliumTemplate130, nil
}
