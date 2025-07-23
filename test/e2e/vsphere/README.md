# VSphere Support for E2E Tests

This package provides VSphere deployment support for the nodeadm e2e tests. VSphere deployment is currently only supported for Bottlerocket OS.

## Overview

The VSphere support allows you to create and delete hybrid nodes running on VMware VSphere infrastructure instead of EC2 instances. This is useful for testing nodeadm in on-premises or hybrid cloud environments.

## Configuration

To use VSphere deployment, you need to configure the VSphere settings in your e2e test configuration file:

```yaml
vsphere:
  server: "vcenter.example.com"
  username: "administrator@vsphere.local"
  password: "your-password"
  datacenter: "Datacenter1"
  cluster: "Cluster1"
  datastore: "datastore1"
  network: "VM Network"
  template: "bottlerocket-template"
```

## Usage

### Creating a VSphere Node

To create a VSphere node, use the `--deployment vsphere` flag with the create command:

```bash
./e2e-test node create my-vsphere-node \
  --config-file config.yaml \
  --deployment vsphere \
  --os bottlerocket \
  --arch amd64 \
  --creds-provider iam-ra
```

**Note**: VSphere deployment is only supported with Bottlerocket OS. Attempting to use other operating systems will result in an error.

### Deleting a VSphere Node

To delete a VSphere node, use the `--deployment vsphere` flag with the delete command:

```bash
./e2e-test node delete my-vsphere-node \
  --config-file config.yaml \
  --deployment vsphere
```

## Implementation Details

### Current Implementation

The implementation provides full VSphere integration using the govmomi library:

- **VM Creation**: Uses govmomi to create actual VSphere VMs from Bottlerocket templates
- **Bottlerocket Configuration**: Generates proper TOML settings using the existing Bottlerocket OS BuildUserData method
- **Network Configuration**: Configures VM network settings to connect to specified VSphere networks
- **User Data**: Passes Bottlerocket settings as base64-encoded user data via guestinfo properties
- **VM Deletion**: Handles VSphere VM cleanup and Kubernetes node removal
- **Kubernetes Integration**: Properly integrates with Kubernetes cluster for node management
- **Logging**: Provides comprehensive logging for all VSphere operations

### Key Features

1. **Real VSphere API**: Uses govmomi library for actual VSphere API calls
2. **Template Management**: Supports Bottlerocket VM templates with proper configuration
3. **Network Configuration**: Configures VM networking for VSphere environments
4. **Storage Management**: Uses specified datastores for VM storage
5. **Resource Management**: Configurable CPU (2 cores) and memory (4GB) settings
6. **Credential Integration**: Supports both SSM and IAM Roles Anywhere credential providers
7. **Proper User Data**: Generates Bottlerocket TOML settings following AWS documentation

## Architecture

The VSphere support is implemented with the following components:

- `VSphereNodeCreate`: Handles VSphere VM creation and configuration
- `VSphereNodeCleanup`: Handles VSphere VM deletion and cleanup
- `VSphereConfig`: Configuration structure for VSphere settings
- `VSphereInstance`: Represents a VSphere VM instance

## Limitations

1. **OS Support**: Currently only supports Bottlerocket OS
2. **Authentication**: Basic username/password authentication only
3. **Resource Configuration**: Fixed CPU (2 cores) and memory (4GB) settings
4. **VM Folder**: Uses default "vm" folder for VM placement

## Security Considerations

When using VSphere deployment:

1. Store VSphere credentials securely (consider using environment variables)
2. Use dedicated service accounts with minimal required permissions
3. Ensure network security between the test environment and VSphere infrastructure
4. Regularly rotate VSphere credentials

## Troubleshooting

### Common Issues

1. **OS Validation Error**: Ensure you're using `--os bottlerocket` when using VSphere deployment
2. **Configuration Missing**: Verify all required VSphere configuration fields are present
3. **Network Connectivity**: Ensure the test environment can reach the VSphere server

### Debug Logging

The VSphere implementation provides detailed logging for troubleshooting:

```bash
# Enable debug logging
export LOG_LEVEL=debug
./e2e-test node create my-vsphere-node --deployment vsphere ...
```

## Contributing

When contributing to VSphere support:

1. Maintain compatibility with the existing e2e test framework
2. Add appropriate unit tests for new functionality
3. Update documentation for any new configuration options
4. Follow the existing code patterns and error handling
