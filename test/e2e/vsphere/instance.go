package vsphere

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// VSphereInstanceConfig holds the configuration for the VSphere VM instance.
type VSphereInstanceConfig struct {
	InstanceName  string
	UserData      []byte
	VSphereConfig *VSphereConfig
	NodeName      string
}

// VSphereInstance represents a VSphere VM instance
type VSphereInstance struct {
	ID   string
	IP   string
	Name string
}

// Create creates a VSphere VM with the provided configuration
func (c *VSphereInstanceConfig) Create(ctx context.Context) (VSphereInstance, error) {
	// Parse VSphere URL
	vSphereURL := fmt.Sprintf("%s/sdk", c.VSphereConfig.Server)
	u, err := url.Parse(vSphereURL)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("parsing VSphere URL: %w", err)
	}
	u.User = url.UserPassword(c.VSphereConfig.Username, c.VSphereConfig.Password)

	// Connect to vSphere
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("creating VSphere client: %w", err)
	}

	// Create finder
	finder := find.NewFinder(client.Client, true)

	// Find datacenter
	dc, err := finder.Datacenter(ctx, c.VSphereConfig.Datacenter)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding datacenter: %w", err)
	}
	finder.SetDatacenter(dc)

	// Find resource pool
	pool, err := finder.ResourcePool(ctx, c.VSphereConfig.ResourcePool)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding resource pool: %w", err)
	}

	// Find datastore
	datastore, err := finder.Datastore(ctx, c.VSphereConfig.Datastore)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding datastore: %w", err)
	}

	// Find template
	template, err := finder.VirtualMachine(ctx, c.VSphereConfig.Template)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding template: %w", err)
	}

	// Find folder
	folder, err := finder.Folder(ctx, c.VSphereConfig.Folder)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("finding folder: %w", err)
	}

	// Configure network
	deviceChanges, err := configureNetworks(ctx, finder, template, c.VSphereConfig.Network)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("configuring networks: %w", err)
	}

	// Base64 encode the userdata (Bottlerocket settings TOML)
	userdataEncoded := base64.StdEncoding.EncodeToString(c.UserData)

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
	task, err := template.Clone(ctx, folder, c.InstanceName, *cloneSpec)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("cloning template: %w", err)
	}

	info, err := task.WaitForResult(ctx, nil)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("waiting for VM creation: %w", err)
	}

	// Get VM reference
	newVM := object.NewVirtualMachine(client.Client, info.Result.(types.ManagedObjectReference))

	// Double-check network connection
	err = connectExistingVMNetwork(ctx, newVM)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("ensuring network connection: %v", err)
	}

	// Wait for IP address
	ip, err := newVM.WaitForIP(ctx)
	if err != nil {
		return VSphereInstance{}, fmt.Errorf("waiting for IP: %v", err)
	}

	return VSphereInstance{
		ID:   c.InstanceName,
		IP:   ip,
		Name: c.NodeName,
	}, nil
}

// connectExistingVMNetwork ensures the VM's network connection is properly configured
func connectExistingVMNetwork(ctx context.Context, vm *object.VirtualMachine) error {
	devices, err := vm.Device(ctx)
	if err != nil {
		return err
	}

	var deviceChanges []types.BaseVirtualDeviceConfigSpec

	for _, dev := range devices.SelectByType((*types.VirtualEthernetCard)(nil)) {
		nic := dev.(types.BaseVirtualEthernetCard)

		// Update connection settings
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

	spec := types.VirtualMachineConfigSpec{
		DeviceChange: deviceChanges,
	}

	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return err
	}

	return task.Wait(ctx)
}

// configureNetworks configures the network settings for the VM
func configureNetworks(ctx context.Context, finder *find.Finder, template *object.VirtualMachine, networkName string) ([]types.BaseVirtualDeviceConfigSpec, error) {
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
