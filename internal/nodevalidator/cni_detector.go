package nodevalidator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	cniConfigDir = "/etc/cni/net.d"
	cniBinDir    = "/opt/cni/bin"
)

// cniDetector implements CNIDetector interface
type cniDetector struct {
	client    kubernetes.Interface
	logger    *zap.Logger
	configDir string
	binDir    string
}

// NewCNIDetector creates a new CNIDetector
func NewCNIDetector(client kubernetes.Interface, logger *zap.Logger) CNIDetector {
	return &cniDetector{
		client:    client,
		logger:    logger,
		configDir: cniConfigDir,
		binDir:    cniBinDir,
	}
}

// DetectCNI checks if a supported CNI plugin is installed and running
func (cd *cniDetector) DetectCNI(ctx context.Context, nodeName string) (CNIType, error) {
	var cniType CNIType
	var err error

	// Validate Static CNI files on node
	cniType, err = cd.detectStaticCNI()
	if err != nil {
		return cniType, err
	}

	// Validate CNI running on node object from the cluster
	if nodeName != "" {
		node, err := cd.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return CNITypeNone, fmt.Errorf("could not get node object for CNI detection: %w", err)
		}

		// Check node.Status.Conditions for NetworkUnavailable condition. Most reliable method set by CNI plugin.
		cniType, err = cd.detectFromNodeCondition(node)
		if err == nil && cniType != CNITypeNone {
			return cniType, nil
		}

		// Check if taint applied (node.Spec.Taints), validate only if node runtime condition failed
		cniType = cd.detectFromNodeTaintsStatus(node)
		if cniType != CNITypeNone {
			return cniType, nil
		}
	}

	return CNITypeNone, errors.New("Unable to detect CNI in Kubelet NetworkUnavailable condition")
}

// detectStaticCNI performs static validation by checking config and binaries files
func (cd *cniDetector) detectStaticCNI() (CNIType, error) {
	// Check if CNI config file exists
	cniType, err := cd.detectFromConfigFiles()
	if err == nil && cniType != CNITypeNone {
		return cniType, nil
	}

	// Check if CNI binary files exists
	cniType, err = cd.detectFromBinaries()
	if err == nil && cniType != CNITypeNone {
		return cniType, nil
	}

	return CNITypeNone, errors.New("no CNI static files on node")
}

// detectFromConfigFiles checks CNI configuration files
func (cd *cniDetector) detectFromConfigFiles() (CNIType, error) {
	if !cd.dirExists(cd.configDir) {
		return CNITypeNone, fmt.Errorf("no directory found: %s", cd.configDir)
	}

	files, err := os.ReadDir(cd.configDir)
	if err != nil {
		return CNITypeNone, fmt.Errorf("failed to read: %s", cd.configDir)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := strings.ToLower(file.Name())

		// Check for Cilium config files
		if strings.Contains(fileName, "cilium") {
			return CNITypeCilium, nil
		}

		// Check for Calico config files
		if strings.Contains(fileName, "calico") {
			return CNITypeCalico, nil
		}
	}

	return CNITypeNone, fmt.Errorf("neither Cilium or Calico found in %s", cd.configDir)
}

// detectFromBinaries checks CNI binaries
func (cd *cniDetector) detectFromBinaries() (CNIType, error) {
	if !cd.dirExists(cd.binDir) {
		return CNITypeNone, fmt.Errorf("no directory found: %s", cd.binDir)
	}

	files, err := os.ReadDir(cd.binDir)
	if err != nil {
		return CNITypeNone, fmt.Errorf("failed to read: %s", cd.binDir)
	}

	hasCilium := false
	hasCalico := false

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := strings.ToLower(file.Name())

		// Check for Cilium binaries
		if strings.Contains(fileName, "cilium") {
			hasCilium = true
		}

		// Check for Calico binaries
		if strings.Contains(fileName, "calico") {
			hasCalico = true
		}
	}

	// Prioritize Cilium if both are present
	if hasCilium {
		return CNITypeCilium, nil
	}
	if hasCalico {
		return CNITypeCalico, nil
	}

	return CNITypeNone, fmt.Errorf("neither Cilium or Calico found in %s", cd.binDir)
}

// detectFromNodeCondition detects CNI from node network connectivity
func (cd *cniDetector) detectFromNodeCondition(node *corev1.Node) (CNIType, error) {
	networkCondition := cd.hasNetworkUnavailableCondition(node)
	if networkCondition == nil {
		return CNITypeNone, fmt.Errorf("node does not have NetworkUnavailable condition")
	}

	// Check if network is available (status should be False)
	if networkCondition.Status != corev1.ConditionFalse {
		return CNITypeNone, fmt.Errorf("network is unavailable: status %s, reason %s", string(networkCondition.Status), networkCondition.Reason)
	}

	// Network is available, check the reason to determine CNI type
	switch networkCondition.Reason {
	case "CiliumIsUp":
		cd.logger.Debug("CNI detected from NetworkUnavailable condition",
			zap.String("reason", networkCondition.Reason))
		return CNITypeCilium, nil
	case "CalicoIsUp":
		cd.logger.Debug("CNI detected from NetworkUnavailable condition",
			zap.String("reason", networkCondition.Reason))
		return CNITypeCalico, nil
	default:
		cd.logger.Debug("NetworkUnavailable condition present but unknown CNI",
			zap.String("reason", networkCondition.Reason),
			zap.String("message", networkCondition.Message))
		return CNITypeNone, fmt.Errorf("unknown CNI: reason %s", networkCondition.Reason)
	}
}

// detectFromNodeTaintsStatus detects CNI type from node taints
func (cd *cniDetector) detectFromNodeTaintsStatus(node *corev1.Node) CNIType {
	// Check for Cilium taint
	if cd.hasCiliumTaint(node) {
		cd.logger.Debug("Cilium detected from taints")
		return CNITypeCilium
	}

	// Check for Calico taint
	if cd.hasCalicoTaint(node) {
		cd.logger.Debug("Calico detected from taints")
		return CNITypeCalico
	}

	return CNITypeNone
}

// hasCiliumTaint checks if node has Cilium-specific taints
func (cd *cniDetector) hasCiliumTaint(node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if strings.Contains(strings.ToLower(taint.Key), "cilium") {
			return true
		}
	}
	return false
}

// hasCalicoTaint checks if node has Calico-specific taints
func (cd *cniDetector) hasCalicoTaint(node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if strings.Contains(strings.ToLower(taint.Key), "calico") {
			return true
		}
	}
	return false
}

// hasNetworkUnavailableCondition checks if node has NetworkUnavailable condition
func (cd *cniDetector) hasNetworkUnavailableCondition(node *corev1.Node) *corev1.NodeCondition {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeNetworkUnavailable {
			return &condition
		}
	}
	return nil
}

// dirExists checks if a directory exists
func (cd *cniDetector) dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// waitForCNIDetection waits for CNI detection
func waitForCNIDetection(ctx context.Context, client kubernetes.Interface, nodeName string, logger *zap.Logger) (CNIType, error) {
	detector := NewCNIDetector(client, logger)
	cniType, err := detector.DetectCNI(ctx, nodeName)
	if err != nil {
		return CNITypeNone, err
	}

	return cniType, nil
}
