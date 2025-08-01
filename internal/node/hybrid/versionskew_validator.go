package hybrid

import (
	"context"
	"fmt"
	"strings"

	"github.com/blang/semver/v4"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

// Kubelet is the kubernetes node agent.
type Kubelet interface {
	// Version returns the current kubelet version
	Version() (string, error)
}

const (
	maxVersionSkew = 3
	remediation    = "Ensure the hybrid node's Kubernetes version follows the version skew policy of the EKS cluster. " +
		"Update the node's Kubernetes components using 'nodeadm upgrade' or reinstall with a compatible version." +
		" https://kubernetes.io/releases/version-skew-policy/#kubelet"
)

// ValidateKubeletVersionSkew validates the version skew for kube-apiserver and kubelet.
func (hnp *HybridNodeProvider) ValidateKubeletVersionSkew(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
	var err error
	if hnp.cluster == nil {
		informer.Starting(ctx, kubeletVersionSkew, "Skipping kubelet version skew validation")
		informer.Done(ctx, kubeletVersionSkew, err)
		return nil
	} else {
		informer.Starting(ctx, kubeletVersionSkew, "Validating kubelet version skew")
		defer func() {
			informer.Done(ctx, kubeletVersionSkew, err)
		}()

		kubeApiServerVersion := *(hnp.cluster.Version)
		kubeletVersion, err := hnp.kubelet.Version()
		if err != nil {
			err = fmt.Errorf("failed to get kubelet version: %w", err)
			err = validation.WithRemediation(err, remediation)
			return err
		}
		apiServerSemver, err := parseK8sVersion(kubeApiServerVersion)
		if err != nil {
			err = fmt.Errorf("failed to parse kube-apiserver version %s: %w", kubeApiServerVersion, err)
			err = validation.WithRemediation(err, remediation)
			return err
		}

		kubeletSemver, err := parseK8sVersion(kubeletVersion)
		if err != nil {
			err = fmt.Errorf("failed to parse kubelet version %s: %w", kubeletVersion, err)
			err = validation.WithRemediation(err, remediation)
			return err
		}

		if kubeletSemver.Minor > apiServerSemver.Minor {
			err = fmt.Errorf("kubelet version %s is newer than kube-apiserver version %s", kubeletVersion, kubeApiServerVersion)
			err = validation.WithRemediation(err, remediation)
			return err
		}

		minorVersionDiff := int(apiServerSemver.Minor - kubeletSemver.Minor)
		if minorVersionDiff > maxVersionSkew {
			err = fmt.Errorf("kubelet version %s is too old for kube-apiserver version %s; maximum supported version skew is %d minor versions",
				kubeletVersion, kubeApiServerVersion, maxVersionSkew)
			err = validation.WithRemediation(err, remediation)
			return err
		}
	}
	return nil
}

// parseK8sVersion parses Kubernetes version strings
func parseK8sVersion(version string) (semver.Version, error) {
	version = strings.TrimPrefix(version, "v")

	parts := strings.Split(version, ".")
	if len(parts) == 2 {
		version += ".0"
	}

	return semver.Parse(version)
}
