package kubernetes

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

// NewServiceAccount creates a new service account in the given namespace, if it does not already exist.
func NewServiceAccount(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) error {
	// Create new service account
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	_, err := ik8s.CreateRetry(ctx, k8s.CoreV1().ServiceAccounts(namespace), sa)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating service account %s in namespace %s: %w", name, namespace, err)
	}

	return nil
}
