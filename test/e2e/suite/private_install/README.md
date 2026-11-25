# Private install E2E suite

Tests the [private installation](https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-private-install.html)
flow on a hybrid node without internet access:

1. Creates an S3 bucket with the default settings (ACLs disabled, Block Public Access on)
   and the bucket policy from the guide, which only allows reads through a VPC endpoint.
2. Runs the `nodeadm sync-artifacts` code against the bucket, and copies the nodeadm
   binary under test and the generated manifest to it.
3. Starts an AL2023 node with `nodeadm install`/`init` in `--private-mode`, using
   `--manifest-override` with the manifest in the bucket, and checks that it joins and
   runs pods.
4. Checks that the node can't reach the internet, and that the manifest can't be read
   from outside the VPC endpoint.

This suite isn't in the default `E2E_SUITES`, so it doesn't run in CI.

## How the node is isolated

The node runs in the shared hybrid node subnet with its own security group. The group
only allows traffic to the hybrid node VPC, the cluster VPC, the pod network, and S3
through a gateway endpoint. Everything else, including the internet, is blocked.

The node reaches AWS services (SSM, STS, ECR, EKS, IAM Roles Anywhere) through interface
endpoints created **without** private DNS. Instead, `/etc/hosts` on the node maps the
service hostnames, and the cluster API server hostname, to private IPs. This keeps
other nodes in the VPC unaffected.

Some things differ from a real air-gapped environment:

- The S3 gateway endpoint is added to the shared hybrid node route table, so S3 traffic
  from other instances in the VPC also goes through it while the suite runs.
- The test runner, not the node, runs `sync-artifacts`, as a customer would from a
  machine with internet access.
- DNS still resolves public names; only connections are blocked.

## Requirements

- A cluster with private endpoint access enabled (`endpointPrivateAccess: true`). The
  node reaches the API server through the control plane network interfaces in the
  cluster VPC. Setup doesn't change an existing cluster, so use a new cluster name.
- The `calico` CNI. Cilium images come from public ECR, which has no VPC endpoint.
- No custom EKS endpoint (`endpoint` in the test config). The suite skips if one is set.

## Running

Build the suite and tools:

```bash
make e2e-test ginkgo
make e2e-tests-binary E2E_SUITES=./test/e2e/suite/private_install
```

Create a setup config:

```yaml
# private-install-setup.yaml
clusterName: nodeadm-e2e-private-install
clusterRegion: us-west-2
kubernetesVersion: "1.35"
cni: calico
endpointPrivateAccess: true
```

Run the suite:

```bash
./_bin/e2e-test run-e2e \
  --setup-config private-install-setup.yaml \
  --tests-binary ./_bin/private_install.test \
  --test-filter "private-install" \
  --artifacts-dir e2e-artifacts
```

The suite deletes the bucket, endpoints and security groups when it finishes. The
sweeper also deletes leaked ones, by the `Nodeadm-E2E-Private-Install` tag and the
`nodeadm-e2e-private-` bucket prefix, before deleting the setup stack.
