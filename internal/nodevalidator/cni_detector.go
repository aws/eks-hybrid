package nodevalidation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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
	client kubernetes.Interface
	logger *zap.Logger
}

// NewCNIDetector creates a new CNIDetector
func NewCNIDetector(client kubernetes.Interface, logger *zap.Logger) CNIDetector {
	return &cniDetector{
		client: client,
		logger: logger,
	}
}

// DetectCNI checks if a supported CNI plugin is installed using hybrid approach
func (cd *cniDetector) DetectCNI(ctx context.Context, nodeName string) (CNIType, error) {
	var cniType CNIType
	var err error

	// Validate CNI based on static files on node
	cniType, err = cd.detectFromBinaries()
	if err != nil {
		cd.logger.Warn("cni binary files detection failed", zap.String("cniType", string(cniType)), zap.Error(err))
	}

	cniType, err = cd.detectFromConfigFiles()
	if err != nil {
		cd.logger.Warn("cni config file detection failed", zap.String("cniType", string(cniType)), zap.Error(err))
	}

	cd.logger.Info("detected cni from static files", zap.String("cniType", string(cniType)))

	// Validate CNI running on node
	if nodeName != "" {
		node, err := cd.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return CNITypeNone, fmt.Errorf("could not get node object for CNI detection: %w", err)
		}

		// Node condition detection
		cniType, err = cd.detectFromNodeCondition(node)
		if err == nil && cniType != CNITypeNone {
			cd.logger.Info("CNI dynamically detected from node condition", zap.String("cniType", string(cniType)))
			return cniType, nil
		}

		// Check if taint applied, validate only if node runtime condition failed
		cniType = cd.detectFromNodeTaintsStatus(node)
		if cniType != CNITypeNone {
			cd.logger.Info("CNI dynamically detected from node taints", zap.String("cniType", string(cniType)))
			return cniType, nil
		}
	}

	return CNITypeNone, errors.New("cni not detected")
}

// detectFromConfigFiles checks CNI configuration files
func (cd *cniDetector) detectFromConfigFiles() (CNIType, error) {
	if !cd.dirExists(cniConfigDir) {
		return CNITypeNone, fmt.Errorf("no directory found: %s", cniConfigDir)
	}

	files, err := os.ReadDir(cniConfigDir)
	if err != nil {
		return CNITypeNone, fmt.Errorf("failed to read: %s", cniConfigDir)
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

	return CNITypeNone, fmt.Errorf("neither Cilium or Calico found in %s", cniConfigDir)
}

// detectFromBinaries checks CNI binaries
func (cd *cniDetector) detectFromBinaries() (CNIType, error) {
	if !cd.dirExists(cniBinDir) {
		return CNITypeNone, fmt.Errorf("no directory found: %s", cniBinDir)
	}

	files, err := os.ReadDir(cniBinDir)
	if err != nil {
		return CNITypeNone, fmt.Errorf("failed to read: %s", cniBinDir)
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

	cd.logger.Info("CNI binaries", zap.Bool("hasCilium", hasCilium), zap.Bool("hasCalico", hasCalico))
	// Prioritize Cilium if both are present
	if hasCilium {
		return CNITypeCilium, nil
	}
	if hasCalico {
		return CNITypeCalico, nil
	}

	return CNITypeNone, fmt.Errorf("neither Cilium or Calico found in %s", cniBinDir)
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
		cd.logger.Info("CNI detected from NetworkUnavailable condition",
			zap.String("reason", networkCondition.Reason))
		return CNITypeCilium, nil
	case "CalicoIsUp":
		cd.logger.Info("CNI detected from NetworkUnavailable condition",
			zap.String("reason", networkCondition.Reason))
		return CNITypeCalico, nil
	default:
		cd.logger.Info("NetworkUnavailable condition present but unknown CNI",
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

// waitForCNIDetection waits for CNI detection with individual retry logic
func waitForCNIDetection(ctx context.Context, client kubernetes.Interface, nodeName string, logger *zap.Logger) (CNIType, error) {
	statusCh := make(chan CNIType)
	errCh := make(chan error)
	consecutiveErrors := 0

	logger.Info("Starting CNI detection validation...")
	go func() {
		defer close(statusCh)
		defer close(errCh)

		for {
			if ctx.Err() != nil {
				return
			}

			// Create CNI detector and execute
			detector := NewCNIDetector(client, logger)
			cniType, err := detector.DetectCNI(ctx, nodeName)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors > 2 || ctx.Err() != nil {
					errCh <- fmt.Errorf("failed all three attempts: %v", err)
					statusCh <- CNITypeNone
					return
				}
				time.Sleep(2 * time.Second)
			} else {
				// Success - CNI detection completed
				statusCh <- cniType
				return
			}
		}
	}()

	select {
	case cniType := <-statusCh:
		logger.Info("CNI detection validation completed successfully", zap.String("cniType", string(cniType)))
		return cniType, nil
	case err := <-errCh:
		return CNITypeNone, err
	case <-ctx.Done():
		return CNITypeNone, fmt.Errorf("CNI detection validation timeout occurred: %w", ctx.Err())
	}
}
