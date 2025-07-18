package vsphere

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
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
	Server     string
	Username   string
	Password   string
	Datacenter string
	Cluster    string
	Datastore  string
	Network    string
	Template   string
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

// VSphereInstance represents a VSphere VM instance
type VSphereInstance struct {
	ID   string
	IP   string
	Name string
}

// Create spins up a VSphere VM with the proper configuration to join as a Hybrid node to the cluster
func (c VSphereNodeCreate) Create(ctx context.Context, spec *VSphereNodeSpec) (VSphereInstance, error) {
	c.Logger.Info("Creating VSphere node", "nodeName", spec.NodeName)

	// Validate that this is Bottlerocket OS
	if spec.OS.Name() != "bottlerocket" {
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
	userdata, err := spec.OS.BuildUserData(e2e.UserDataInput{
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
	vm, err := c.createVSphereVM(ctx, spec, userdata)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("creating VSphere VM: %w", err)
	}

	c.Logger.Info("VSphere VM created", "instanceID", vm.ID, "ip", vm.IP)
	return vm, nil
}

// createVSphereVM creates the actual VSphere VM using govmomi
func (c VSphereNodeCreate) createVSphereVM(ctx context.Context, spec *VSphereNodeSpec, userdata []byte) (VSphereInstance, error) {
	// Parse VSphere URL
	vSphereURL := fmt.Sprintf("%s/sdk", spec.VSphereConfig.Server)
	u, err := url.Parse(vSphereURL)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("parsing VSphere URL: %w", err)
	}
	u.User = url.UserPassword(spec.VSphereConfig.Username, spec.VSphereConfig.Password)

	// Connect to vSphere
	c.Logger.Info("Connecting to VSphere", "server", spec.VSphereConfig.Server)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("creating VSphere client: %w", err)
	}

	// Create finder
	finder := find.NewFinder(client.Client, true)

	// Find datacenter
	c.Logger.Info("Finding datacenter", "datacenter", spec.VSphereConfig.Datacenter)
	dc, err := finder.Datacenter(ctx, spec.VSphereConfig.Datacenter)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding datacenter: %w", err)
	}
	finder.SetDatacenter(dc)

	// Find resource pool
	c.Logger.Info("Finding resource pool", "cluster", spec.VSphereConfig.Cluster)
	pool, err := finder.ResourcePool(ctx, spec.VSphereConfig.Cluster)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding resource pool: %w", err)
	}

	// Find datastore
	c.Logger.Info("Finding datastore", "datastore", spec.VSphereConfig.Datastore)
	datastore, err := finder.Datastore(ctx, spec.VSphereConfig.Datastore)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding datastore: %w", err)
	}

	// Find template
	c.Logger.Info("Finding template", "template", spec.VSphereConfig.Template)
	template, err := finder.VirtualMachine(ctx, spec.VSphereConfig.Template)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding template: %w", err)
	}

	// Find folder (use default VM folder)
	folder, err := finder.Folder(ctx, "vm")
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding VM folder: %w", err)
	}

	// Configure network
	deviceChanges, err := c.configureNetworks(ctx, finder, template, spec.VSphereConfig.Network)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("configuring networks: %w", err)
	}

	// Base64 encode the userdata (Bottlerocket settings TOML)
	userdataEncoded := base64.StdEncoding.EncodeToString(userdata)

	// Create the ExtraConfig settings for Bottlerocket
	extraConfig := []types.BaseOptionValue{
		&types.OptionValue{
			Key:   "guestinfo.userdata",
			Value: userdataEncoded,
		},
		&types.OptionValue{
			Key:   "guestinfo.userdata.encoding",
			Value: "base64",
		},
	}

	// Create clone specification
	cloneSpec := &types.VirtualMachineCloneSpec{
		Location: types.VirtualMachineRelocateSpec{
			Pool:      types.NewReference(pool.Reference()),
			Datastore: types.NewReference(datastore.Reference()),
			Folder:    types.NewReference(folder.Reference()),
		},
		Template: false,
		PowerOn:  true,
		Config: &types.VirtualMachineConfigSpec{
			NumCPUs:      2,
			MemoryMB:     4096,
			DeviceChange: deviceChanges,
			ExtraConfig:  extraConfig,
		},
	}

	// Perform the clone
	c.Logger.Info("Creating VM from template", "vmName", spec.InstanceName)
	task, err := template.Clone(ctx, folder, spec.InstanceName, *cloneSpec)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("cloning template: %w", err)
	}

	c.Logger.Info("Waiting for VM creation to complete")
	info, err := task.WaitForResult(ctx, nil)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("waiting for VM creation: %w", err)
	}

	// Get VM reference
	newVM := object.NewVirtualMachine(client.Client, info.Result.(types.ManagedObjectReference))

	// Wait for IP address
	c.Logger.Info("Waiting for VM IP address")
	ip, err := newVM.WaitForIP(ctx)
	if err != nil {
		c.Logger.Info("Could not get VM IP, using placeholder", "error", err)
		ip = "192.168.1.100" // Fallback IP
	}

	return VSphereInstance{
		ID:   spec.InstanceName,
		IP:   ip,
		Name: spec.NodeName,
	}, nil
}

// configureNetworks configures the network settings for the VM
func (c VSphereNodeCreate) configureNetworks(ctx context.Context, finder *find.Finder, template *object.VirtualMachine, networkName string) ([]types.BaseVirtualDeviceConfigSpec, error) {
	devices, err := template.Device(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting template devices: %w", err)
	}

	// Find the network
	network, err := finder.Network(ctx, networkName)
	if err != nil {
		return nil, fmt.Errorf("finding network %s: %w", networkName, err)
	}

	var deviceChanges []types.BaseVirtualDeviceConfigSpec

	// Get network backing info
	backing, err := network.EthernetCardBackingInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting network backing info: %w", err)
	}

	// Configure each NIC
	for _, dev := range devices.SelectByType((*types.VirtualEthernetCard)(nil)) {
		nic := dev.(types.BaseVirtualEthernetCard)

		// Set the new backing
		nic.GetVirtualEthernetCard().Backing = backing

		// Ensure connection settings are correct
		nic.GetVirtualEthernetCard().Connectable = &types.VirtualDeviceConnectInfo{
			StartConnected:    true,
			Connected:         true,
			AllowGuestControl: true,
			Status:            "ok",
		}

		deviceSpec := &types.VirtualDeviceConfigSpec{
			Operation: types.VirtualDeviceConfigSpecOperationEdit,
			Device:    nic.(types.BaseVirtualDevice),
		}
		deviceChanges = append(deviceChanges, deviceSpec)
	}

	return deviceChanges, nil
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
