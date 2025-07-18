package vsphere

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

const (
	govcURLEnvVar      = "GOVC_URL"
	govcUsernameEnvVar = "GOVC_USERNAME"
	govcPasswordEnvVar = "GOVC_PASSWORD"
	sshKeyEnvVar       = "SSH_KEY"
)

func main() {
	// vSphere connection parameters
	vSphereURL := fmt.Sprintf("%s/sdk", os.Getenv(govcURLEnvVar))
	username := os.Getenv(govcUsernameEnvVar)
	password := os.Getenv(govcPasswordEnvVar)

	// Parse URL
	u, err := url.Parse(vSphereURL)
	if err != nil {
		log.Fatal(err)
	}
	u.User = url.UserPassword(username, password)

	// Create context
	ctx := context.Background()

	// Connect to vSphere
	fmt.Println("Creating govc client")
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		fmt.Println("Error creating govc client")
		log.Fatal(err)
	}

	// Create finder
	fmt.Println("Creating Finder")
	finder := find.NewFinder(client.Client, true)

	// Find datacenter
	fmt.Println("Finding Datacenter")
	dc, err := finder.Datacenter(ctx, "SDDC-Datacenter")
	if err != nil {
		log.Fatal(err)
	}
	finder.SetDatacenter(dc)

	// Find host or cluster
	fmt.Println("Finding resource pool")
	pool, err := finder.ResourcePool(ctx, "Compute-ResourcePool")
	if err != nil {
		log.Fatal(err)
	}

	// Find folder
	fmt.Println("Finding folder")
	folder, err := finder.Folder(ctx, "arnchlm") // Use "vm" for the default VM folder
	if err != nil {
		log.Fatal(err)
	}

	// Find datastore
	fmt.Println("Finding datastore")
	datastore, err := finder.Datastore(ctx, "WorkloadDatastore")
	if err != nil {
		log.Fatal(err)
	}

	// Find template
	fmt.Println("Finding template")
	template, err := finder.VirtualMachine(ctx, "bottlerocket-kube-v1-32") // Use "vm" for the default VM folder
	if err != nil {
		log.Fatal(err)
	}

	deviceChanges, err := configureNetworks(ctx, finder, template)
	if err != nil {
		log.Fatal(err)
	}

	sshKeyFile := os.Getenv(sshKeyEnvVar)

	sshKey, err := os.ReadFile(sshKeyFile)
	if err != nil {
		log.Fatal(err)
	}

	sshConfig := fmt.Sprintf(`{"ssh":{"authorized-keys":["%s"]}}`, strings.TrimSpace(string(sshKey)))

	sshKeyEncoded := base64.StdEncoding.EncodeToString([]byte(sshConfig))

	userdataToml := fmt.Sprintf(`[settings.host-containers.admin]
enabled = true
user-data = %q`, sshKeyEncoded,
	)

	userdataTomlEncoded := base64.StdEncoding.EncodeToString([]byte(userdataToml))

	// Create the ExtraConfig settings
	extraConfig := []types.BaseOptionValue{
		&types.OptionValue{
			Key:   "guestinfo.userdata",
			Value: userdataTomlEncoded,
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
		// Customize guest OS if needed
		Customization: &types.CustomizationSpec{
			Identity: &types.CustomizationLinuxPrep{
				HostName: &types.CustomizationFixedName{
					Name: "test-vm",
				},
				Domain:   "domain.local",
				TimeZone: "UTC",
			},
			NicSettingMap: []types.CustomizationAdapterMapping{
				{
					Adapter: types.CustomizationIPSettings{
						Ip: &types.CustomizationDhcpIpGenerator{}, // or use static IP
					},
				},
			},
		},
	}

	// Perform the clone
	fmt.Println("Creating VM from template")
	task, err := template.Clone(ctx, folder, "test-vm", *cloneSpec)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Cloning in progress...")

	// Wait for task completion
	fmt.Println("Waiting for VM creation")
	info, err := task.WaitForResult(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Get VM reference
	newVM := object.NewVirtualMachine(client.Client, info.Result.(types.ManagedObjectReference))
	name, err := newVM.ObjectName(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Created VM: %+v\n", name)

	// Wait for the VM to be ready
	time.Sleep(10 * time.Second)

	// Double-check network connection
	err = connectExistingVMNetwork(ctx, newVM)
	if err != nil {
		log.Printf("Error ensuring network connection: %v", err)
	}

	// Optional: Wait for IP address
	ip, err := newVM.WaitForIP(ctx)
	if err != nil {
		fmt.Printf("Error waiting for IP: %v\n", err)
	} else {
		fmt.Printf("VM IP: %s\n", ip)
	}
}

func configureNetworks(ctx context.Context, finder *find.Finder, template *object.VirtualMachine) ([]types.BaseVirtualDeviceConfigSpec, error) {
	devices, err := template.Device(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting devices: %v", err)
	}

	// Find the network
	network, err := finder.Network(ctx, "sddc-cgw-network-1") // Replace with your network name
	if err != nil {
		return nil, fmt.Errorf("error finding network: %v", err)
	}

	var deviceChanges []types.BaseVirtualDeviceConfigSpec

	// Get network backing info
	backing, err := network.EthernetCardBackingInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting backing info: %v", err)
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

// If you need to fix an existing VM's network connection:
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
