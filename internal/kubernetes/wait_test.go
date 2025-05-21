package kubernetes_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/kubernetes"
)

func TestWaitFor(t *testing.T) {
	type PodGetter = mockGetter[*corev1.Pod] // Assuming mockGetter is accessible or redefined

	readyPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	notReadyPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodPending}}

	isPodReady := func(pod *corev1.Pod) bool {
		if pod == nil {
			return false
		}
		return pod.Status.Phase == corev1.PodRunning
	}

	tests := []struct {
		name                 string
		podName              string
		waitForTimeout       time.Duration
		ctxTimeout           time.Duration // For external context cancellation
		ctxCanceler          func(context.CancelFunc)
		setupGetter          func(g Gomega, mg *PodGetter)
		readyFunc            func(*corev1.Pod) bool
		expectedPod          *corev1.Pod
		expectedErrSubstring string
	}{
		{
			name:           "success on first try",
			podName:        "pod1",
			waitForTimeout: 2 * time.Second, // Sufficiently long for one attempt
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					return readyPod, nil
				}
			},
			readyFunc:   isPodReady,
			expectedPod: readyPod,
		},
		{
			name:           "success after a few retries",
			podName:        "pod2",
			waitForTimeout: 2 * time.Second,
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					mg.callCount++
					if mg.callCount < 3 {
						return notReadyPod, nil
					}
					return readyPod, nil
				}
			},
			readyFunc:   isPodReady,
			expectedPod: readyPod,
		},
		{
			name:           "failure due to WaitFor timeout",
			podName:        "pod3",
			waitForTimeout: 10 * time.Millisecond, // Short WaitFor timeout
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					return notReadyPod, nil // Always not ready
				}
			},
			readyFunc:            isPodReady,
			expectedErrSubstring: context.DeadlineExceeded.Error(), // Error from retrier.Do's internal context
		},
		{
			name:           "failure due to getter error max 3 consecutive",
			podName:        "pod4",
			waitForTimeout: 1 * time.Second,
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					// Fail 4 times consecutively
					return nil, errInternalGetter
				}
			},
			readyFunc:            isPodReady,
			expectedErrSubstring: fmt.Sprintf("max attempts 3 reached: %s", errInternalGetter),
		},
		{
			name:           "success despite intermittent getter errors",
			podName:        "pod5",
			waitForTimeout: 2 * time.Second,
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					mg.callCount++
					switch mg.callCount {
					case 1: // Error
						return nil, errInternalGetter
					case 2: // Not ready
						return notReadyPod, nil
					case 3: // Error again
						return nil, errInternalGetter
					default: // Ready
						return readyPod, nil
					}
				}
			},
			readyFunc:   isPodReady,
			expectedPod: readyPod,
		},
		{
			name:           "external context cancellation",
			podName:        "pod6",
			waitForTimeout: 2 * time.Second,        // Long enough WaitFor timeout
			ctxTimeout:     100 * time.Millisecond, // Short external context timeout
			ctxCanceler: func(cancel context.CancelFunc) {
				go func() {
					time.Sleep(2 * time.Millisecond)
					cancel()
				}()
			},
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					return notReadyPod, nil
				}
			},
			readyFunc:            isPodReady,
			expectedErrSubstring: context.Canceled.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			getter := &PodGetter{}
			tt.setupGetter(g, getter)

			ctx := context.Background()
			var cancel context.CancelFunc
			if tt.ctxTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			} else {
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
			}
			if tt.ctxCanceler != nil {
				tt.ctxCanceler(cancel)
			}

			read := func(ctx context.Context) (*corev1.Pod, error) {
				return getter.Get(ctx, tt.podName, metav1.GetOptions{})
			}

			pod, err := kubernetes.WaitFor(ctx, tt.waitForTimeout, read, tt.readyFunc)

			if tt.expectedErrSubstring == "" {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(pod).ToNot(BeNil())
				g.Expect(pod).To(BeComparableTo(tt.expectedPod)) // For non-nil expectedPod
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErrSubstring))
			}
		})
	}
}

func TestGetAndWait(t *testing.T) {
	podName := "test-get-and-wait"
	readyPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}

	isPodReady := func(pod *corev1.Pod) bool {
		return pod != nil && pod.Status.Phase == corev1.PodRunning
	}

	t.Run("positive case - get succeeds and object is ready", func(t *testing.T) {
		g := NewWithT(t)
		getter := &mockGetter[*corev1.Pod]{}
		getter.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
			g.Expect(name).To(Equal(podName))
			return readyPod, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		pod, err := kubernetes.GetAndWait(ctx, 500*time.Millisecond, getter, podName, isPodReady)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(pod).To(Equal(readyPod))
		g.Expect(getter.callCount).To(Equal(1))
	})

	t.Run("negative case - get fails leading to timeout", func(t *testing.T) {
		g := NewWithT(t)
		getter := &mockGetter[*corev1.Pod]{}
		getter.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
			return nil, errInternalGetter // Always fail
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond) // Outer context for the whole test
		defer cancel()

		_, err := kubernetes.GetAndWait(ctx, 50*time.Millisecond, getter, podName, isPodReady)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring(errInternalGetter.Error()))
	})
}

func TestListAndWait(t *testing.T) {
	readyPodList := &corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}}}

	isPodListReady := func(podList *corev1.PodList) bool {
		return podList != nil && len(podList.Items) > 0 && podList.Items[0].Status.Phase == corev1.PodRunning
	}

	t.Run("positive case - list succeeds and object is ready", func(t *testing.T) {
		g := NewWithT(t)
		lister := &mockLister[*corev1.PodList]{}
		lister.listFunc = func(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
			return readyPodList, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		list, err := kubernetes.ListAndWait(ctx, 500*time.Millisecond, lister, isPodListReady)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(list).To(Equal(readyPodList))
		g.Expect(lister.callCount).To(Equal(1))
	})

	t.Run("negative case - list fails leading to timeout", func(t *testing.T) {
		g := NewWithT(t)
		lister := &mockLister[*corev1.PodList]{}
		lister.listFunc = func(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
			return nil, errInternalLister // Always fail
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err := kubernetes.ListAndWait(ctx, 50*time.Millisecond, lister, isPodListReady)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring(errInternalLister.Error()))
	})

	t.Run("list options are properly applied", func(t *testing.T) {
		g := NewWithT(t)
		lister := &mockLister[*corev1.PodList]{}
		expectedLabelSelector := "app=test"
		expectedFieldSelector := "metadata.name=pod1"
		expectedLimit := int64(10)

		lister.listFunc = func(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
			g.Expect(options.LabelSelector).To(Equal(expectedLabelSelector))
			g.Expect(options.FieldSelector).To(Equal(expectedFieldSelector))
			g.Expect(options.Limit).To(Equal(expectedLimit))
			return readyPodList, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Create list options using the kubernetes package's ListOption functions
		opts := []kubernetes.ListOption{
			func(o *kubernetes.ListOptions) {
				o.LabelSelector = expectedLabelSelector
			},
			func(o *kubernetes.ListOptions) {
				o.FieldSelector = expectedFieldSelector
			},
			func(o *kubernetes.ListOptions) {
				o.Limit = expectedLimit
			},
		}

		list, err := kubernetes.ListRetry(ctx, lister, opts...)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(list).To(Equal(readyPodList))
		g.Expect(lister.callCount).To(Equal(1))
	})
}
