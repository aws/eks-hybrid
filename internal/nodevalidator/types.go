package nodevalidation

import (
	"context"
)

// CNIType represents the type of CNI plugin detected
type CNIType string

const (
	CNITypeNone   CNIType = "none"
	CNITypeCilium CNIType = "cilium"
	CNITypeCalico CNIType = "calico"
)

// NodeRegistrationChecker interface for checking node registration with Kubernetes cluster
type NodeRegistrationChecker interface {
	// WaitForNodeRegistration waits for the node to register with the Kubernetes cluster
	// Returns the node name if successful, or an error if the timeout is reached
	WaitForNodeRegistration(ctx context.Context) (string, error)
}

// CNIDetector interface for detecting CNI plugin type
type CNIDetector interface {
	// DetectCNI checks if a supported CNI plugin is installed
	// Returns the detected CNI type or CNITypeNone if no supported CNI is detected
	DetectCNI(ctx context.Context, nodeName string) (CNIType, error)
}

// NodeReadinessChecker interface for checking node readiness
type NodeReadinessChecker interface {
	// WaitForNodeReadiness waits for the node to become ready
	// Returns nil if successful, or an error if the timeout is reached
	WaitForNodeReadiness(ctx context.Context, nodeName string) error
}
