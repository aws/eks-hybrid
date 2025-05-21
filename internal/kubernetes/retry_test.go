package kubernetes_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/aws/eks-hybrid/internal/kubernetes"
)

var (
	errInternalGetter  = errors.New("internal getter error")
	errInternalLister  = errors.New("internal lister error")
	errInternalDeleter = errors.New("internal deleter error")
	errInternalCreator = errors.New("internal creator error")
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

// mockDeleter is a mock implementation of the Deleter interface.
type mockDeleter struct {
	deleteFunc func(ctx context.Context, name string, options metav1.DeleteOptions) error
	callCount  int
}

func (m *mockDeleter) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	m.callCount++
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, name, options)
	}
	return errors.New("mockDeleter.Delete not implemented or called unexpectedly")
}

func TestDeleteRetry(t *testing.T) {
	tests := []struct {
		name         string
		podName      string
		setupDeleter func(g Gomega, md *mockDeleter)
		ctxTimeout   time.Duration
		expectedErr  string
	}{
		{
			name:    "success on first try",
			podName: "my-pod",
			setupDeleter: func(g Gomega, md *mockDeleter) {
				md.deleteFunc = func(ctx context.Context, name string, options metav1.DeleteOptions) error {
					g.Expect(name).To(Equal("my-pod"))
					return nil
				}
			},
		},
		{
			name:    "not found error should not be retried",
			podName: "not-found-pod",
			setupDeleter: func(g Gomega, md *mockDeleter) {
				md.deleteFunc = func(ctx context.Context, name string, options metav1.DeleteOptions) error {
					g.Expect(name).To(Equal("not-found-pod"))
					return apierrors.NewNotFound(corev1.Resource("pod"), name)
				}
			},
			expectedErr: "pod \"not-found-pod\" not found",
		},
		{
			name:    "other errors should be retried",
			podName: "error-pod",
			setupDeleter: func(g Gomega, md *mockDeleter) {
				md.deleteFunc = func(ctx context.Context, name string, options metav1.DeleteOptions) error {
					g.Expect(name).To(Equal("error-pod"))
					return errInternalDeleter
				}
			},
			ctxTimeout:  2 * time.Second,
			expectedErr: errInternalDeleter.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			deleter := &mockDeleter{}
			tt.setupDeleter(g, deleter)

			ctx := context.Background()
			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			err := kubernetes.DeleteRetry(ctx, deleter, tt.podName)

			if tt.expectedErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(deleter.callCount).To(Equal(1))
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedErr)))
				// For not found errors, we expect exactly one call
				// For other errors, we expect multiple calls due to retries
				if apierrors.IsNotFound(err) {
					g.Expect(deleter.callCount).To(Equal(1))
				} else {
					g.Expect(deleter.callCount).To(BeNumerically(">", 1))
				}
			}
		})
	}
}

// mockCreator is a mock implementation of the Creator interface.
type mockCreator[O runtime.Object] struct {
	createFunc func(ctx context.Context, obj O, options metav1.CreateOptions) (O, error)
	callCount  int
}

func (m *mockCreator[O]) Create(ctx context.Context, obj O, options metav1.CreateOptions) (O, error) {
	m.callCount++
	if m.createFunc != nil {
		return m.createFunc(ctx, obj, options)
	}
	var zero O
	return zero, errors.New("mockCreator.Create not implemented or called unexpectedly")
}

func TestCreateRetry(t *testing.T) {
	type PodCreator = mockCreator[*corev1.Pod]

	tests := []struct {
		name         string
		pod          *corev1.Pod
		setupCreator func(g Gomega, mc *PodCreator)
		ctxTimeout   time.Duration
		expectedPod  *corev1.Pod
		expectedErr  string
	}{
		{
			name: "success on first try",
			pod:  &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "my-pod"}},
			setupCreator: func(g Gomega, mc *PodCreator) {
				mc.createFunc = func(ctx context.Context, obj *corev1.Pod, options metav1.CreateOptions) (*corev1.Pod, error) {
					g.Expect(obj.Name).To(Equal("my-pod"))
					return obj, nil
				}
			},
			expectedPod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "my-pod"}},
		},
		{
			name: "already exists error should not be retried",
			pod:  &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "existing-pod"}},
			setupCreator: func(g Gomega, mc *PodCreator) {
				mc.createFunc = func(ctx context.Context, obj *corev1.Pod, options metav1.CreateOptions) (*corev1.Pod, error) {
					g.Expect(obj.Name).To(Equal("existing-pod"))
					return nil, apierrors.NewAlreadyExists(corev1.Resource("pod"), obj.Name)
				}
			},
			expectedErr: "pod \"existing-pod\" already exists",
		},
		{
			name: "other errors should be retried",
			pod:  &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "error-pod"}},
			setupCreator: func(g Gomega, mc *PodCreator) {
				mc.createFunc = func(ctx context.Context, obj *corev1.Pod, options metav1.CreateOptions) (*corev1.Pod, error) {
					g.Expect(obj.Name).To(Equal("error-pod"))
					return nil, errInternalCreator
				}
			},
			ctxTimeout:  2 * time.Second,
			expectedErr: errInternalCreator.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			creator := &PodCreator{}
			tt.setupCreator(g, creator)

			ctx := context.Background()
			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			pod, err := kubernetes.CreateRetry(ctx, creator, tt.pod)

			if tt.expectedErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(pod).To(BeComparableTo(tt.expectedPod))
				g.Expect(creator.callCount).To(Equal(1))
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedErr)))
				// For already exists errors, we expect exactly one call
				// For other errors, we expect multiple calls due to retries
				if apierrors.IsAlreadyExists(err) {
					g.Expect(creator.callCount).To(Equal(1))
				} else {
					g.Expect(creator.callCount).To(BeNumerically(">", 1))
				}
			}
		})
	}
}
