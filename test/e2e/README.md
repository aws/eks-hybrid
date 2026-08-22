# E2E Testing Guide

This guide explains how to run end-to-end (E2E) tests locally and outlines the key principles of our E2E testing framework.

## Core Principles

1. **Idempotent Setup and Cleanup**: The setup and cleanup commands are designed to be rerunnable without causing errors or duplicate resources.

2. **Self-Cleaning Tests**: Each test is responsible for cleaning up any resources it creates during execution.

3. **Comprehensive Resource Management**: When new infrastructure resources are added to tests, they must also be handled by the sweeper cleanup mechanism.

4. **Strategic Retry Management**: 
   - Infrastructure operations (AWS API calls, Kubernetes API operations) must implement appropriate retry mechanisms with exponential backoff
   - Test verification steps should include retries with reasonable timeouts to handle eventual consistency
   - nodeadm operations must NOT include retries in test code - any reliability improvements for nodeadm operations must be implemented within nodeadm itself to ensure accurate product behavior assessment


## Running E2E Tests Locally

## Using the run-e2e Subcommand

The `run-e2e` subcommand provides a streamlined way to run E2E tests with configurable options. Here's a basic example:

1. 
    ```bash
    make e2e-test ginkgo
    ```
2. 
   ```bash
   ./_bin/e2e-test run-e2e -f="al23-amd64 && simpleflow && ssm" --skip-cleanup=true --artifacts-dir=e2e-artifacts --logs-bucket=<s3 logs bucket> 
   ```

3. Passing --skip-cleanup allows for quickly reruning additional test cases without having to wait for new cluster creation/teardown.

4. After running `run-e2e` to create your test infrastructure, you could use the following when rerunning:
   ```bash
    ./_bin/ginkgo  -v -tags=e2e --label-filter='al23-amd64 && simpleflow && ssm'  ./test/e2e/suite/nodeadm -- -filepath=e2e-artifacts/configs/e2e-param.yaml
   ```

### Key Options

- `-f, --test-filter`: Filter tests using ginkgo label filters (e.g., "al23-amd64 && simpleflow && ssm")
- `--skip-cleanup`: Skip cleanup after test completion
- `--artifacts-dir`: Directory to store test artifacts (defaults to a temporary directory)
- `--cni`: CNI plugin to use (default: "cilium")
- `--logs-bucket`: S3 bucket for uploading test logs
- `--no-color`: Disable colored output
- `-n, --name`: Cluster name (default: "nodeadm-e2e-tests-1-34")
- `-r, --region`: AWS region (default: "us-west-2")
- `-k, --kubernetes-version`: Kubernetes version (default: "1.34")
- `-p, --procs`: Number of processes to run (default: 1)
- `--timeout`: Test timeout (default: "60m")
- `--setup-config`: Path to YAML file containing cluster.TestResources configuration
- `--test-config`: Path to YAML file containing suite.TestConfig configuration
- `--ginkgo-binary`: Path to the ginkgo binary (defaults to binary in same folder as e2e-test or in PATH)


You can run the tests manually using the CLI commands:

1. **make**:
    ```bash
    make e2e-test ginkgo
    ```

2. **Setup the test infrastructure**:
   ```bash
   ./_bin/e2e-test setup -s path/to/e2e-config.yaml
   ```

3. **Run a single test**:
   ```bash
    ./_bin/ginkgo  -v -tags=e2e --label-filter='al23-amd64 && simpleflow && ssm'  ./test/e2e/suite/nodeadm -- -filepath=path/to/e2e-param.yaml
   ```

4. **Clean up the infrastructure**:
   ```bash
   # Using the resources file (for specific cleanup)
   ./_bin/e2e-test cleanup -f path/to/resources.yaml
    ```

## Configuration Files

### Cluster Resources Configuration (e2e-config.yaml)

This file defines the infrastructure to be created for testing:

```yaml
clusterName: nodeadm-e2e-tests-1-34
clusterRegion: us-west-2
clusterNetwork:
  vpcCidr: 10.0.0.0/16
  publicSubnetCidr: 10.0.10.0/24
  privateSubnetCidr: 10.0.20.0/24
hybridNetwork:
  vpcCidr: 10.1.0.0/16
  publicSubnetCidr: 10.1.1.0/24
  privateSubnetCidr: 10.1.2.0/24
  podCidr: 10.2.0.0/16
kubernetesVersion: "1.34"
cni: cilium
endpoint: ""
```

* cni - cilium or calico
* endpoint - optional, intended to be used for testing against beta or other environments


### Test Parameters Configuration (e2e-param.yaml)

This file configures the test execution parameters:

```yaml
clusterName: nodeadm-e2e-tests-1-34
clusterRegion: us-west-2
nodeadmUrlAMD: https://hybrid-assets.eks.amazonaws.com/releases/latest/bin/linux/amd64/nodeadm
nodeadmUrlARM: https://hybrid-assets.eks.amazonaws.com/releases/latest/bin/linux/arm64/nodeadm
setRootPassword: false
logsBucket: ""
endpoint: ""
artifactsFolder: ""
```

* setRootPassword: optional, if true, newly created EC2 instances will have a randomly set root password for logging into
* logsBucket: optional, if set, test will collect logs bundle and upload to bucket
* endpoint - optional, intended to be used for testing against beta or other environments
* artifactsFolder: optional, if set, tests boot logs/junit/json ginkgo output will be written to, otherwise a tmp folder is used


* Note: the above files could be combined into one and the folder `e2e-config` is in the gitignore and is a good place to store these files.

## Cleanup Options

### Using the Cleanup Command

The cleanup command requires a resources file and removes specific resources:

```bash
./_bin/e2e-test cleanup --filename path/to/resources.yaml
```

### Using the Sweeper Command

The sweeper command provides more flexible cleanup options:

```bash
# Clean up a specific cluster
./_bin/e2e-test sweeper --cluster-name nodeadm-e2e-tests-1-34

# Clean up clusters with a specific prefix
./_bin/e2e-test sweeper --cluster-prefix nodeadm-e2e-tests- --age-threshold 12h

# Clean up all test clusters older than 24 hours
./_bin/e2e-test sweeper --all --age-threshold 24h

# Dry run to see what would be deleted without making changes
./_bin/e2e-test sweeper --cluster-prefix nodeadm-e2e-tests- --dry-run
```

## Managing Test Nodes

### Creating Test Nodes

The `create` subcommand allows you to create individual test nodes for your E2E test cluster. This is useful for testing specific node configurations or when you need to add nodes to an existing cluster.

Basic usage:
```bash
./_bin/e2e-test create <INSTANCE_NAME> -f path/to/e2e-param.yaml
```

Key options:
- `-f, --config-file`: Path to the test configuration file (required)
- `-c, --creds-provider`: Credentials provider to use (iam-ra, ssm)
- `-o, --os`: Operating system to use (al23, ubuntu2004, ubuntu2204, ubuntu2404, rhel8, rhel9)
- `-a, --arch`: Architecture to use (amd64, arm64)
- `-w, --wait-for-ready`: Wait for the node to be ready in the cluster

Example:
```bash
# Create an Amazon Linux 2023 AMD64 node with SSM credentials
./_bin/e2e-test create test-node-1 -f e2e-artifacts/configs/e2e-param.yaml -c ssm -o al23 -a amd64 -w

# Create an Ubuntu 22.04 ARM64 node with IAM Roles Anywhere credentials
./_bin/e2e-test create test-node-2 -f e2e-artifacts/configs/e2e-param.yaml -c iam-ra -o ubuntu2204 -a arm64
```

Note: For RHEL nodes, you need to set the following environment variables:
- `RHEL_USERNAME`: Your Red Hat subscription username
- `RHEL_PASSWORD`: Your Red Hat subscription password

### SSH into Test Nodes

The `ssh` subcommand allows you to connect to test nodes through the jumpbox instance. This is useful for debugging or manual testing.

Basic usage:
```bash
./_bin/e2e-test ssh <INSTANCE_ID>
```

Example:
```bash
# SSH into a node using its instance ID
./_bin/e2e-test ssh i-0123456789abcdef0
```

The command will:
1. Find the instance in AWS
2. Identify the cluster it belongs to
3. Locate the jumpbox instance
4. Establish an SSH connection through SSM

Note: The SSH connection is established through AWS Systems Manager Session Manager, so you don't need to manage SSH keys or security groups directly.

