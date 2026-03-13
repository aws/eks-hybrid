package kubernetes

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CSIVolumeHandleFromPVC returns the CSI volume handle for the PV bound to the given PVC.
func CSIVolumeHandleFromPVC(ctx context.Context, k8s kubernetes.Interface, namespace, pvcName string) (string, error) {
	pvc, err := k8s.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting PVC %s: %w", pvcName, err)
	}

	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return "", fmt.Errorf("PVC %s has no bound PV", pvcName)
	}

	pv, err := k8s.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting PV %s: %w", pvName, err)
	}

	if pv.Spec.CSI == nil {
		return "", fmt.Errorf("PV %s has no CSI spec", pvName)
	}

	volumeHandle := pv.Spec.CSI.VolumeHandle
	if volumeHandle == "" {
		return "", fmt.Errorf("PV %s has no CSI volume handle", pvName)
	}

	return volumeHandle, nil
}
