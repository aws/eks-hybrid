package kubernetes_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/aws/eks-hybrid/internal/kubernetes"
)

var (
	errInternalGetter = errors.New("internal getter error")
	errInternalLister = errors.New("internal lister error")
)

func TestGetRetry(t *testing.T) {
	// Define a concrete type for O in tests, e.g., *corev1.Pod
	// This makes it easier to work with the mock and expected values.
	type PodGetter = mockGetter[*corev1.Pod]

	tests := []struct {
		name        string
		podName     string
		setupGetter func(g Gomega, mg *PodGetter) // Function to configure the mock getter
		ctxTimeout  time.Duration                 // 0 for no explicit test-level timeout on context
		expectedPod *corev1.Pod                   // The pod expected to be returned
		expectedErr string                        // Substring of the expected error, empty if no error
	}{
		{
			name:    "success with non-nil object",
			podName: "my-pod",
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					g.Expect(name).To(Equal("my-pod"))
					return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "my-pod"}}, nil
				}
			},
			expectedPod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "my-pod"}},
		},
		{
			name:    "success with nil object and nil error from getter",
			podName: "nil-pod",
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					g.Expect(name).To(Equal("nil-pod"))
					return nil, nil // Getter returns nil object, nil error
				}
			},
			expectedPod: nil, // Expecting a nil pod back
		},
		{
			name:    "error case with short context timeout",
			podName: "error-pod",
			setupGetter: func(g Gomega, mg *PodGetter) {
				mg.getFunc = func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
					return nil, errInternalGetter
				}
			},
			ctxTimeout:  10 * time.Millisecond, // Short timeout for the test context
			expectedPod: nil,
			// NetworkRequest wraps the last error with the context error when its derived context is interrupted.
			expectedErr: errInternalGetter.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			getter := &PodGetter{}
			tt.setupGetter(g, getter)

			ctx := context.Background()
			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			pod, err := kubernetes.GetRetry(ctx, getter, tt.podName)

			if tt.expectedErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
				if tt.expectedPod == nil {
					g.Expect(pod).To(BeNil())
				} else {
					g.Expect(pod).ToNot(BeNil())
					g.Expect(pod).To(BeComparableTo(tt.expectedPod))
				}
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedErr)))
				g.Expect(pod).To(BeNil())
			}
		})
	}
}

func TestListRetry(t *testing.T) {
	type PodListLister = mockLister[*corev1.PodList]

	tests := []struct {
		name         string
		setupLister  func(g Gomega, ml *PodListLister)
		ctxTimeout   time.Duration
		expectedList *corev1.PodList
		expectedErr  string
	}{
		{
			name: "success with non-nil list",
			setupLister: func(g Gomega, ml *PodListLister) {
				ml.listFunc = func(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
					return &corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}}}}, nil
				}
			},
			expectedList: &corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}}}},
		},
		{
			name: "success with nil list and nil error from lister",
			setupLister: func(g Gomega, ml *PodListLister) {
				ml.listFunc = func(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
					return nil, nil
				}
			},
			expectedList: nil,
		},
		{
			name: "error case with short context timeout",
			setupLister: func(g Gomega, ml *PodListLister) {
				ml.listFunc = func(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
					return nil, errInternalLister
				}
			},
			ctxTimeout:   10 * time.Millisecond,
			expectedList: nil,
			expectedErr:  errInternalLister.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			lister := &PodListLister{}
			tt.setupLister(g, lister)

			ctx := context.Background()
			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			list, err := kubernetes.ListRetry(ctx, lister)

			if tt.expectedErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
				if tt.expectedList == nil {
					g.Expect(list).To(BeNil())
				} else {
					g.Expect(list).ToNot(BeNil())
					g.Expect(list).To(BeComparableTo(tt.expectedList))
				}
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedErr)))
				g.Expect(list).To(BeNil())
			}
		})
	}
}

// mockGetter is a mock implementation of the Getter interface.
type mockGetter[O runtime.Object] struct {
	getFunc   func(ctx context.Context, name string, options metav1.GetOptions) (O, error)
	callCount int
}

func (m *mockGetter[O]) Get(ctx context.Context, name string, options metav1.GetOptions) (O, error) {
	m.callCount++
	if m.getFunc != nil {
		return m.getFunc(ctx, name, options)
	}
	var zero O
	// This default error helps identify if a test case forgot to set up getFunc.
	return zero, errors.New("mockGetter.Get not implemented or called unexpectedly")
}

// mockLister is a mock implementation of the Lister interface.
type mockLister[O runtime.Object] struct {
	listFunc  func(ctx context.Context, options metav1.ListOptions) (O, error)
	callCount int
}

func (m *mockLister[O]) List(ctx context.Context, options metav1.ListOptions) (O, error) {
	m.callCount++
	if m.listFunc != nil {
		return m.listFunc(ctx, options)
	}
	var zero O
	return zero, errors.New("mockLister.List not implemented or called unexpectedly")
}
