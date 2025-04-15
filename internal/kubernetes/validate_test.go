package kubernetes_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	fakeTesting "k8s.io/client-go/testing"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/validation"
)

type fakeKubeconfig struct {
	client clientgo.Interface
	err    error
}

func (f fakeKubeconfig) Path() string {
	return "/var/lib/kubelet/kubeconfig"
}

func (f fakeKubeconfig) BuildClient() (clientgo.Interface, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.client, nil
}

func TestAPIServerValidator_MakeAuthenticatedRequest(t *testing.T) {
	testCases := []struct {
		name            string
		objs            []runtime.Object
		wantErr         string
		wantRemediation string
	}{
		{
			name: "success",
			objs: []runtime.Object{
				&corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
				},
			},
		},
		{
			name:            "no endpoints",
			wantErr:         "endpoints \"kubernetes\" not found",
			wantRemediation: "Verify the Kubernetes identity and permissions assigned to the IAM roles on this node, it should belong to the group 'system:nodes'. Check your Access Entries or aws-auth ConfigMap.",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			g := NewWithT(t)
			client := fake.NewSimpleClientset(tc.objs...)
			informer := test.NewFakeInformer()

			nodeConfig := &api.NodeConfig{}
			kubelet := newMockKubelet(client, "v1.28.0")

			v := kubernetes.NewAPIServerValidator(fakeKubeconfig{client: client})
			err := v.MakeAuthenticatedRequest(ctx, informer, nodeConfig)
			if tc.wantErr == "" {
				g.Expect(err).To(BeNil())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantErr)))
			}

			g.Expect(informer.Started).To(BeTrue())
			g.Expect(validation.Remediation(informer.DoneWith)).To(Equal(tc.wantRemediation))
		})
	}
}

func TestAPIServerValidator_MakeAuthenticatedRequest_FailBuildingClient(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	informer := test.NewFakeInformer()

	nodeConfig := &api.NodeConfig{}
	kubelet := newMockKubelet(nil, "v1.28.0")
	kubelet.clientError = errors.New("can't build client")

	v := kubernetes.NewAPIServerValidator(fakeKubeconfig{err: errors.New("can't build client")})
	err := v.MakeAuthenticatedRequest(ctx, informer, nodeConfig)

	g.Expect(err).To(MatchError(ContainSubstring("can't build client")))

	g.Expect(informer.Started).To(BeTrue())
	g.Expect(validation.Remediation(informer.DoneWith)).To(Equal("Ensure the kubeconfig at /var/lib/kubelet/kubeconfig has been created and is valid."))
}

func TestAPIServerValidator_CheckVPCEndpointAccess(t *testing.T) {
	gg := NewWithT(t)
	server := test.NewHTTPSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	serverPort, err := server.Port()
	gg.Expect(err).NotTo(HaveOccurred())

	testCases := []struct {
		name            string
		objs            []runtime.Object
		wantErr         string
		wantRemediation string
	}{
		{
			name: "empty kubernetes endpoints",
			objs: []runtime.Object{
				&corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
				},
			},
			wantErr: "no subsets found in the Kubernetes endpoint, can't validate VPC API server access",
		},
		{
			name: "no access to IP",
			objs: []runtime.Object{
				&corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
								},
							},
						},
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "",
									Hostname: "kubernetes.server",
								},
								{
									IP: "127.0.0.1",
								},
							},
							Ports: []corev1.EndpointPort{
								{
									Name: "other-protocol",
									Port: int32(serverPort),
								},
								{
									Name: "https",
									Port: 12345,
								},
							},
						},
					},
				},
			},
			wantErr:         "dialing 127.0.0.1:12345: dial tcp 127.0.0.1:12345: connect: connection refused",
			wantRemediation: "Ensure the node has access to the Kube-API server endpoint 127.0.0.1 in the VPC",
		},
		{
			name:            "no endpoints",
			wantErr:         "endpoints \"kubernetes\" not found",
			wantRemediation: "Verify the Kubernetes identity and permissions assigned to the IAM roles on this node, it should belong to the group 'system:nodes'. Check your Access Entries or aws-auth ConfigMap.",
		},
		{
			name: "Success",
			objs: []runtime.Object{
				&corev1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kubernetes",
						Namespace: "default",
					},
					Subsets: []corev1.EndpointSubset{
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP: "1.2.3.4",
								},
							},
						},
						{
							Addresses: []corev1.EndpointAddress{
								{
									IP:       "",
									Hostname: "kubernetes.server",
								},
								{
									IP: "127.0.0.1",
								},
							},
							Ports: []corev1.EndpointPort{
								{
									Name: "https",
									Port: int32(serverPort),
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			g := NewWithT(t)
			client := fake.NewSimpleClientset(tc.objs...)
			informer := test.NewFakeInformer()

			nodeConfig := &api.NodeConfig{}
			kubelet := newMockKubelet(client, "v1.28.0")

			v := kubernetes.NewAPIServerValidator(fakeKubeconfig{client: client})
			err := v.CheckVPCEndpointAccess(ctx, informer, nodeConfig)
			if tc.wantErr == "" {
				g.Expect(err).To(BeNil())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantErr)))
			}

			g.Expect(informer.Started).To(BeTrue())
			g.Expect(validation.Remediation(informer.DoneWith)).To(Equal(tc.wantRemediation))
		})
	}
}

func TestAPIServerValidator_CheckIdentity(t *testing.T) {
	testCases := []struct {
		name                string
		selfReview          *authenticationv1.SelfSubjectReview
		kubeletVersion      string
		wantErr             string
		wantRemediation     string
		wantInformerStarted bool
	}{
		{
			name: "success",
			selfReview: &authenticationv1.SelfSubjectReview{
				Status: authenticationv1.SelfSubjectReviewStatus{
					UserInfo: authenticationv1.UserInfo{
						Username: "system:node:my-node",
						Extra: map[string]authenticationv1.ExtraValue{
							"arn": {"arn:aws:iam::123456789012:role/eks-node-role"},
						},
						Groups: []string{"system:nodes"},
					},
				},
			},
			kubeletVersion:      "v1.28.0",
			wantInformerStarted: true,
		},
		{
			name: "not in group system:nodes",
			selfReview: &authenticationv1.SelfSubjectReview{
				Status: authenticationv1.SelfSubjectReviewStatus{
					UserInfo: authenticationv1.UserInfo{
						Username: "system:node:my-node",
						Extra: map[string]authenticationv1.ExtraValue{
							"arn": {"arn:aws:iam::123456789012:role/eks-node-role"},
						},
						Groups: []string{"system:admin"},
					},
				},
			},
			kubeletVersion:      "v1.28.1",
			wantErr:             "node identity system:node:my-node for principal arn:aws:iam::123456789012:role/eks-node-role does not belong to the group 'system:nodes'",
			wantRemediation:     "Verify the Kubernetes identity and permissions assigned to the IAM roles on this node, it should belong to the group 'system:nodes'. Check your Access Entries or aws-auth ConfigMap.",
			wantInformerStarted: true,
		},
		{
			name: "not a node",
			selfReview: &authenticationv1.SelfSubjectReview{
				Status: authenticationv1.SelfSubjectReviewStatus{
					UserInfo: authenticationv1.UserInfo{
						Username: "my-node",
						Extra: map[string]authenticationv1.ExtraValue{
							"arn": {"arn:aws:iam::123456789012:role/eks-node-role"},
						},
						Groups: []string{"system:nodes"},
					},
				},
			},
			kubeletVersion:      "v1.29.0",
			wantErr:             "node identity my-node for principal arn:aws:iam::123456789012:role/eks-node-role does not match a node identity, username should start with 'system:node:'",
			wantRemediation:     "Verify the Kubernetes identity and permissions assigned to the IAM roles on this node, it should belong to the group 'system:nodes'. Check your Access Entries or aws-auth ConfigMap.",
			wantInformerStarted: true,
		},
		{
			name:                "client failed",
			kubeletVersion:      "v1.28.0",
			wantErr:             "can't execute self review",
			wantRemediation:     "Verify the Kubernetes identity and permissions assigned to the IAM roles on this node, it should belong to the group 'system:nodes'. Check your Access Entries or aws-auth ConfigMap.",
			wantInformerStarted: true,
		},
		{
			name:                "skip validation for 1.25.1",
			kubeletVersion:      "v1.25.1",
			wantInformerStarted: false,
		},
		{
			name:                "skip validation for 1.26.5",
			kubeletVersion:      "v1.26.5",
			wantInformerStarted: false,
		},
		{
			name:                "skip validation for 1.27",
			kubeletVersion:      "v1.27.0",
			wantInformerStarted: false,
		},

		{
			name:                "skip validation for invalid kubelet version",
			kubeletVersion:      "1.50.xx",
			wantInformerStarted: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			g := NewWithT(t)
			client := fake.NewSimpleClientset()
			mockSelfSubjectReview(client, tc.selfReview)
			informer := test.NewFakeInformer()

			nodeConfig := &api.NodeConfig{}
			kubelet := newMockKubelet(client, tc.kubeletVersion)

			v := kubernetes.NewAPIServerValidator(fakeKubeconfig{client: client})
			err := v.CheckIdentity(ctx, informer, nodeConfig)
			if tc.wantErr == "" {
				g.Expect(err).To(BeNil())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantErr)))
			}

			g.Expect(informer.Started).To(Equal(tc.wantInformerStarted))
			g.Expect(validation.Remediation(informer.DoneWith)).To(Equal(tc.wantRemediation))
		})
	}
}

func mockSelfSubjectReview(client *fake.Clientset, selfReview *authenticationv1.SelfSubjectReview) {
	client.PrependReactor("create", "selfsubjectreviews", func(action fakeTesting.Action) (bool, runtime.Object, error) {
		createAction := action.(fakeTesting.CreateAction)
		_, ok := createAction.GetObject().(*authenticationv1.SelfSubjectReview)
		if ok {
			if selfReview == nil {
				return true, nil, errors.New("can't execute self review")
			}
			return true, selfReview, nil
		}

		return true, createAction.GetObject(), nil
	})
}

// mockKubelet implements the Kubelet interface for testing
type mockKubelet struct {
	client         kubernetes.Interface
	version        string
	versionError   error
	clientError    error
	kubeconfigPath string
}

func newMockKubelet(client kubernetes.Interface, version string) *mockKubelet {
	return &mockKubelet{
		client:         client,
		version:        version,
		kubeconfigPath: "/var/lib/kubelet/kubeconfig",
	}
}

func (m *mockKubelet) BuildClient() (kubernetes.Interface, error) {
	if m.clientError != nil {
		return nil, m.clientError
	}
	return m.client, nil
}

func (m *mockKubelet) KubeconfigPath() string {
	return m.kubeconfigPath
}

func (m *mockKubelet) Version() (string, error) {
	if m.versionError != nil {
		return "", m.versionError
	}
	return m.version, nil
}
