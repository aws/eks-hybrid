package node

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/internal/kubelet"
)

type podFilter func([]corev1.Pod) ([]corev1.Pod, error)

func daemonSetFilter(pods []corev1.Pod) ([]corev1.Pod, error) {
	var filteredPods []corev1.Pod

	for _, pod := range pods {
		controllerRef := metav1.GetControllerOf(&pod)
		if controllerRef == nil || controllerRef.Kind != appsv1.SchemeGroupVersion.WithKind("DaemonSet").Kind {
			filteredPods = append(filteredPods, pod)
		}
	}
	return filteredPods, nil
}

func staticPodsFilter(pods []corev1.Pod) ([]corev1.Pod, error) {
	var filteredPods []corev1.Pod

	staticPods, err := getStaticPodsOnNode()
	if err != nil {
		return nil, err
	}

	// If there are no static pods, there is nothing to filter
	if len(staticPods) == 0 {
		return pods, nil
	}
	for _, pod := range pods {
		if !slices.Contains(staticPods, pod.Name) {
			filteredPods = append(filteredPods, pod)
		}
	}
	return filteredPods, nil
}

func getDrainedPodFilters() []podFilter {
	return []podFilter{
		daemonSetFilter,
		staticPodsFilter,
	}
}

func getStaticPodsOnNode() ([]string, error) {
	var staticPodNames []string
	files, err := os.ReadDir(defaultStaticPodManifestPath)
	if err != nil {
		// If manifest directory doesn't exist, there are no static pods.
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, errors.Wrap(err, "failed to read static manifest directory")
	}
	for _, file := range files {
		extension := filepath.Ext(file.Name())
		if extension == ".yaml" || extension == ".yml" {
			fileData, err := os.ReadFile(filepath.Join(defaultStaticPodManifestPath, file.Name()))
			if err != nil {
				return nil, err
			}

			var obj metav1.ObjectMeta
			if err := yaml.Unmarshal(fileData, &obj); err != nil {
				return nil, errors.Wrapf(err, "failed to unmarshal static pod manifest file: %s", file.Name())
			}
			staticPodNames = append(staticPodNames, obj.Name)
		}
	}
	return staticPodNames, nil
}

func getPodsOnNode(ctx context.Context, nodeName string) ([]corev1.Pod, error) {
	clientset, err := kubelet.GetKubeClientFromKubeConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx,
		metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list all pods running on the node")
	}

	return pods.Items, nil
}
