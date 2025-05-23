package kubernetes

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

const namespaceWaitTimeout = 3 * time.Minute

// WaitForNamespaceToBeDeleted waits for a namespace to be deleted up to 3 minutes
func WaitForNamespaceToBeDeleted(ctx context.Context, k8s kubernetes.Interface, name string) error {
	_, err := ik8s.ListAndWait(ctx, namespaceWaitTimeout, k8s.CoreV1().Namespaces(), func(nsList *corev1.NamespaceList) bool {
		// Return true when list is empty, indicating deletion is complete
		return len(nsList.Items) == 0
	}, func(opts *ik8s.ListOptions) {
		opts.FieldSelector = "metadata.name=" + name
	})
	if err != nil {
		return fmt.Errorf("waiting for namespace %s to be deleted: %w", name, err)
	}
	return nil
}
