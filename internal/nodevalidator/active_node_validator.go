package nodevalidator

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/validation"
)

type ActiveNodeValidator struct {
	validateRegistration bool
	validateCNI          bool
	validateReadiness    bool
	timeout              time.Duration
}

// NewActiveNodeValidator creates a new ActiveNodeValidator with default settings
func NewActiveNodeValidator(opts ...func(*ActiveNodeValidator)) ActiveNodeValidator {
	v := &ActiveNodeValidator{
		validateRegistration: true,
		validateCNI:          true,
		validateReadiness:    true,
		timeout:              5 * time.Minute, // Default timeout
	}
	for _, opt := range opts {
		opt(v)
	}
	return *v
}

// configures whether to validate node registration
func WithNodeRegistration(validate bool) func(*ActiveNodeValidator) {
	return func(v *ActiveNodeValidator) {
		v.validateRegistration = validate
	}
}

// configures whether to validate CNI detection
func WithCNIDetection(validate bool) func(*ActiveNodeValidator) {
	return func(v *ActiveNodeValidator) {
		v.validateCNI = validate
	}
}

// configures whether to validate node readiness
func WithNodeReadiness(validate bool) func(*ActiveNodeValidator) {
	return func(v *ActiveNodeValidator) {
		v.validateReadiness = validate
	}
}

// configures the timeout for validations
func WithTimeout(timeout time.Duration) func(*ActiveNodeValidator) {
	return func(v *ActiveNodeValidator) {
		v.timeout = timeout
	}
}

func (v ActiveNodeValidator) Run(ctx context.Context, informer validation.Informer, nodeConfig *api.NodeConfig) error {
	var err error
	var hostname string
	name := "active-node-validation"
	log := logger.FromContext(ctx)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	informer.Starting(ctx, name, "Validating active node status in Kubernetes cluster")
	defer func() {
		informer.Done(ctx, name, err)
	}()

	// Create Kubernetes client using kubelet
	kubeletInstance := kubelet.New()
	k8sClient, err := kubeletInstance.BuildClient()
	if err != nil {
		err = validation.WithRemediation(err,
			"Ensure kubelet is properly configured with valid kubeconfig and the API server is accessible.")
		return err
	}

	// Node Registration validation
	if v.validateRegistration {
		hostname, err = waitForNodeRegistrationValidation(ctx, k8sClient, v.timeout, log)
		if err != nil || hostname == "" {
			if hostname == "" {
				hostname = "null"
			}
			err = validation.WithRemediation(err,
				fmt.Sprintf("Detected Hostname: %s, verify this node's network connectivity and authentication credentials.", hostname))
			return err
		}
	}

	// CNI Detection validation
	if v.validateCNI {
		_, err = waitForCNIDetection(ctx, k8sClient, hostname, log)
		if err != nil {
			err = validation.WithRemediation(err,
				"Ensure a supported Container Network Interface (CNI) plugin is installed and its pods are running correctly in the cluster.")
			return err
		}
	}

	// Node Readiness validation
	if v.validateReadiness {
		err = waitForNodeReadiness(ctx, k8sClient, hostname, v.timeout, log)
		if err != nil {
			err = validation.WithRemediation(err,
				"Check kubelet logs and ensure the node has joined the cluster properly.")
			return err
		}
	}

	return nil
}
