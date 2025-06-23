package kubernetes

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/mod/semver"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/retry"
	"github.com/aws/eks-hybrid/internal/validation"
)

// Kubelet is the kubernetes node agent.
type Kubelet interface {
	// BuildClient creates a new Kubernetes client
	BuildClient() (kubernetes.Interface, error)
	// KubeconfigPath returns the path to the kubeconfig file
	KubeconfigPath() string
	// Version returns the current kubelet version
	Version() (string, error)
}

type APIServerValidator struct {
	kubeconfig Kubeconfig
	kubelet    Kubelet
}

// Kubeconfig defines the interface for working with kubeconfig files
type Kubeconfig interface {
	// Path returns the path to the kubeconfig file
	Path() string
	// BuildClient builds a Kubernetes client from the kubeconfig
	BuildClient() (kubernetes.Interface, error)
}

func NewAPIServerValidator(kubeconfig Kubeconfig, kubelet Kubelet) APIServerValidator {
	return APIServerValidator{
		kubeconfig: kubeconfig,
		kubelet:    kubelet,
	}
}

const badPermissionsRemediation = "Verify the Kubernetes identity and permissions assigned to the IAM roles on this node, it should belong to the group 'system:nodes'. Check your Access Entries or aws-auth ConfigMap."

func (a APIServerValidator) MakeAuthenticatedRequest(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	name := "kubernetes-authenticated-request"
	var err error
	informer.Starting(ctx, name, "Validating authenticated request to Kubernetes API endpoint")
	defer func() {
		informer.Done(ctx, name, err)
	}()
	client, err := a.client()
	if err != nil {
		return err
	}

	_, err = GetRetry(ctx, client.CoreV1().Endpoints("default"), "kubernetes")
	if err != nil {
		err = validation.WithRemediation(
			fmt.Errorf("making authenticated request to Kubernetes API endpoint: %w", err),
			badPermissionsRemediation,
		)
		return err
	}

	return nil
}

func (a APIServerValidator) CheckIdentity(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	var err error
	kubeletVersion, err := a.kubelet.Version()
	if err != nil {
		return err
	}

	// 1.27 and below don't allow SelfSubjectReview requests from nodes
	if semver.Compare(kubeletVersion, "v1.28.0") < 0 {
		return nil
	}

	name := "kubernetes-node-identity"
	informer.Starting(ctx, name, "Validating Kubernetes identity matches a Node identity")
	defer func() {
		informer.Done(ctx, name, err)
	}()

	client, err := a.client()
	if err != nil {
		return err
	}

	self := &authenticationv1.SelfSubjectReview{}

	err = retry.NetworkRequest(ctx, func(ctx context.Context) error {
		var err error
		self, err = client.AuthenticationV1().SelfSubjectReviews().Create(ctx, self, metav1.CreateOptions{})
		return err
	})
	if err != nil {
		err = validation.WithRemediation(err, badPermissionsRemediation)
		return err
	}

	if !slices.Contains(self.Status.UserInfo.Groups, "system:nodes") {
		err = validation.WithRemediation(
			fmt.Errorf(
				"node identity %s for principal %s does not belong to the group 'system:nodes'",
				self.Status.UserInfo.Username, principalARN(self),
			),
			badPermissionsRemediation,
		)

		return err
	}

	if !strings.HasPrefix(self.Status.UserInfo.Username, "system:node:") {
		err = validation.WithRemediation(
			fmt.Errorf("node identity %s for principal %s does not match a node identity, username should start with 'system:node:'",
				self.Status.UserInfo.Username, principalARN(self),
			),
			badPermissionsRemediation,
		)

		return err
	}

	return nil
}

func principalARN(self *authenticationv1.SelfSubjectReview) string {
	var principal string
	principals := self.Status.UserInfo.Extra["arn"]
	if len(principals) > 0 {
		principal = principals[0]
	}

	return principal
}

func (a APIServerValidator) CheckVPCEndpointAccess(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	name := "kubernetes-vpc-api-server-access"
	var err error
	informer.Starting(ctx, name, "Validating access to Kube-API server through VPC IPs")
	defer func() {
		informer.Done(ctx, name, err)
	}()
	client, err := a.client()
	if err != nil {
		return err
	}

	kubeEndpoint, err := GetRetry(ctx, client.CoreV1().Endpoints("default"), "kubernetes")
	if err != nil {
		err = validation.WithRemediation(err, badPermissionsRemediation)
		return err
	}

	if len(kubeEndpoint.Subsets) == 0 {
		err = errors.New("no subsets found in the Kubernetes endpoint, can't validate VPC API server access")
		return err
	}

	for _, subset := range kubeEndpoint.Subsets {
		var port int32
		for _, p := range subset.Ports {
			if p.Name == "https" {
				port = p.Port
				break
			}
		}
		if port == 0 {
			continue
		}

		for _, address := range subset.Addresses {
			if address.IP == "" {
				continue
			}
			u := url.URL{
				Scheme: "https",
				Host:   fmt.Sprintf("%s:%d", address.IP, port),
			}

			err = retry.NetworkRequest(ctx, func(ctx context.Context) error {
				return network.CheckConnectionToHost(ctx, u)
			})
			if err != nil {
				err = validation.WithRemediation(err,
					fmt.Sprintf("Ensure the node has access to the Kube-API server endpoint %s in the VPC", address.IP),
				)
				return err
			}
		}
	}

	return nil
}

func (a APIServerValidator) client() (kubernetes.Interface, error) {
	client, err := a.kubeconfig.BuildClient()
	if err != nil {
		return nil, validation.WithRemediation(err, fmt.Sprintf("Ensure the kubeconfig at %s has been created and is valid.", a.kubeconfig.Path()))
	}

	return client, nil
}
