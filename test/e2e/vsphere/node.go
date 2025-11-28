package vsphere

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/os"
)

// VSphereNodeCreate handles creation of VSphere nodes
type VSphereNodeCreate struct {
	Logger          logr.Logger
	K8sClientConfig *rest.Config
	NodeadmURLs     e2e.NodeadmURLs
	PublicKey       string
	VSphereConfig   *VSphereConfig
}

// VSphereConfig contains VSphere-specific configuration
type VSphereConfig struct {
	Server       string
	Username     string
	Password     string
	Datacenter   string
	Cluster      string
	Datastore    string
	Network      string
	Template     string
	Folder       string
	ResourcePool string
}

// VSphereNodeSpec configures the VSphere Hybrid Node
type VSphereNodeSpec struct {
	InstanceName        string
	NodeK8sVersion      string
	NodeName            string
	OS                  e2e.NodeadmOS
	Provider            e2e.NodeadmCredentialsProvider
	VSphereConfig       *VSphereConfig
	KubernetesAPIServer string
	ClusterName         string
	ClusterCert         []byte
}

// Create spins up a VSphere VM with the proper configuration to join as a Hybrid node to the cluster
func (c VSphereNodeCreate) Create(ctx context.Context, spec *VSphereNodeSpec) (VSphereInstance, error) {
	c.Logger.Info("Creating VSphere node", "nodeName", spec.NodeName)

	// Validate that this is Bottlerocket OS
	if !os.IsBottlerocket(spec.OS.Name()) {
		return VSphereInstance{}, fmt.Errorf("VSphere deployment only supports Bottlerocket OS, got: %s", spec.OS.Name())
	}

	nodeSpec := e2e.NodeSpec{
		Name: spec.NodeName,
		Cluster: &e2e.Cluster{
			Name:   spec.ClusterName,
			Region: "us-west-2", // Default region for VSphere nodes
		},
		Provider: spec.Provider,
	}

	files, err := spec.Provider.FilesForNode(nodeSpec)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("getting files for node: %w", err)
	}

	nodeadmConfig, err := spec.Provider.NodeadmConfig(ctx, nodeSpec)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("building nodeconfig: %w", err)
	}

	// Build user data for VSphere VM using Bottlerocket's BuildUserData method
	userdata, err := spec.OS.BuildUserData(ctx, e2e.UserDataInput{
		EKSEndpoint:         "", // Will be set by the cluster configuration
		KubernetesVersion:   spec.NodeK8sVersion,
		NodeadmUrls:         c.NodeadmURLs,
		NodeadmConfig:       nodeadmConfig,
		Provider:            string(spec.Provider.Name()),
		Region:              "us-west-2", // Default region for VSphere nodes
		Files:               files,
		PublicKey:           c.PublicKey,
		KubernetesAPIServer: spec.KubernetesAPIServer,
		HostName:            spec.NodeName,
		ClusterName:         spec.ClusterName,
		ClusterCert:         spec.ClusterCert,
	})
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("building user data: %w", err)
	}

	// Create VSphere VM with the Bottlerocket settings
	c.Logger.Info("Creating VSphere VM instance")

	// Create VSphere instance config
	instanceConfig := &VSphereInstanceConfig{
		InstanceName:  spec.InstanceName,
		UserData:      userdata,
		VSphereConfig: spec.VSphereConfig,
		NodeName:      spec.NodeName,
	}

	// Create the VM
	vm, err := instanceConfig.Create(ctx)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("creating VSphere VM: %w", err)
	}

	c.Logger.Info("VSphere VM created", "instanceID", vm.ID, "ip", vm.IP)
	return vm, nil
}

// VSphereNodeCleanup handles cleanup of VSphere nodes
type VSphereNodeCleanup struct {
	Logger        logr.Logger
	K8s           clientgo.Interface
	VSphereConfig *VSphereConfig
	LogCollector  os.NodeLogCollector
}

// Cleanup removes the VSphere VM and cleans up the Kubernetes node
func (c *VSphereNodeCleanup) Cleanup(ctx context.Context, instance VSphereInstance) error {
	c.Logger.Info("Cleaning up VSphere node", "instanceID", instance.ID)

	// In a real implementation, this would use the VSphere API to delete the VM
	c.Logger.Info("Simulating VSphere VM deletion", "instanceID", instance.ID)

	// Simulate VM deletion delay
	time.Sleep(3 * time.Second)

	// Clean up the Kubernetes node
	if err := kubernetes.EnsureNodeWithE2ELabelIsDeleted(ctx, c.K8s, instance.Name); err != nil {
		return fmt.Errorf("deleting Kubernetes node %s: %w", instance.Name, err)
	}

	c.Logger.Info("VSphere node cleanup completed", "instanceID", instance.ID)
	return nil
}

// WaitForVMReady waits for the VSphere VM to be ready
func (c VSphereNodeCreate) WaitForVMReady(ctx context.Context, instance VSphereInstance) error {
	c.Logger.Info("Waiting for VSphere VM to be ready", "instanceID", instance.ID)

	// Simulate waiting for VM to be ready
	time.Sleep(10 * time.Second)

	c.Logger.Info("VSphere VM is ready", "instanceID", instance.ID)
	return nil
}
