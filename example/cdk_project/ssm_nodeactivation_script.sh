#!/bin/bash

# Check if the required arguments are provided
if [ "$#" -ne 6 ]; then
    echo "Usage: $0 <private_key> <hosts_file> <activation_id> <activation_code> <cluster_name> <cluster_region>"
    exit 1
fi

private_key_path="$1"
hosts_file="$2"
activation_id="$3"
activation_code="$4"
cluster_name="$5"
region="$6"

cat > nodeConfig.yaml << EOF
apiVersion: node.eks.aws/v1alpha1
kind: NodeConfig
spec:
    cluster:
        name: $cluster_name
        region: $region
    hybrid:
        ssm:
            activationCode: $activation_code
            activationId: $activation_id
EOF

# Loop through each host
while IFS=',' read -r username host os
do
    echo "Connecting to $host as $username (OS: $os)..."

    # change command based on OS
    if [ $os != "redhat" ]; then
        prefix="sudo ./nodeadm"
    else
        prefix="sudo /tmp/nodeadm"
    fi

    # Transfer the file via scp to the host
    scp -i "$private_key_path" nodeConfig.yaml "$username@$host:/home/$username"

    # Use SSH to execute commands on the remote host
    ssh -n -i "$private_key_path" "$username@$host" "
        $prefix init -c file://nodeConfig.yaml
        exit 0
    "
    echo "Activation for $host ...Done."
done < "$hosts_file"
rm nodeConfig.yaml