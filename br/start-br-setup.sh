#!/usr/bin/env bash
set -e
set -x

declare -r PERSISTENT_STORAGE_BASE_DIR="/.bottlerocket/host-containers/current"
declare -r USER_DATA="${PERSISTENT_STORAGE_BASE_DIR}/user-data"

if [[ "$(yq '.iamra'  ${USER_DATA})" == "true" ]]; then
    echo "iam-ra"

    readarray files < <(yq e -o=j -I=0 '.write_files[]' ${USER_DATA} )

    for file in "${files[@]}"; do
        content=$(echo "$file" | yq e '.content' -)
        path=$(echo "$file" | yq e '.path' -)

        mkdir -p $(dirname $path)
        echo "$content" > $path    
    done

    # copy created pki files via cloud-init to var/lib
    mkdir -p /.bottlerocket/rootfs/var/lib/eks-hybrid/roles-anywhere/pki/
    cp /etc/roles-anywhere/pki/node.* /.bottlerocket/rootfs/var/lib/eks-hybrid/roles-anywhere/pki/

    mkdir -p /.bottlerocket/rootfs/var/lib/eks-hybrid/bin/.overlay/{upper,work}
    mv /static-sh /.bottlerocket/rootfs/var/lib/eks-hybrid/bin/.overlay/upper/sh
    chmod -R 775 /.bottlerocket/rootfs/var/lib/eks-hybrid/bin

    # add static build of sh to bin on host for iam-auth to be able to exec aws-singing-helper
    sudo nsenter -t 1 -a -- mount -t overlay overlay -o rw,nosuid,nodev,noatime,context=system_u:object_r:os_t:s0,lowerdir=/bin,upperdir=/var/lib/eks-hybrid/bin/.overlay/upper,workdir=/var/lib/eks-hybrid/bin/.overlay/work /x86_64-bottlerocket-linux-gnu/sys-root/usr/bin
else
    # wait for ssm to register in the control container and copy the aws config to the host and set hostname
    echo "ssm"
    while [ ! -f /.bottlerocket/rootfs/run/host-containerd/io.containerd.runtime.v2.task/default/control/rootfs/root/.aws/credentials ]; do sleep 1; done
    while ! AWS_SHARED_CREDENTIALS_FILE=/.bottlerocket/rootfs/run/host-containerd/io.containerd.runtime.v2.task/default/control/rootfs/root/.aws/credentials aws sts get-caller-identity; do sleep 1; done
    while [ ! -f /.bottlerocket/rootfs//local/host-containers/control/ssm/registration ]; do sleep 1; done
    hostname="$(jq -r ".ManagedInstanceID" /.bottlerocket/rootfs//local/host-containers/control/ssm/registration)"

    apiclient set kubernetes.hostname-override=$hostname
    apiclient set network.hostname=$hostname

    cluster_name="$(apiclient get settings.kubernetes.cluster-name | jq -r ".settings.kubernetes.\"cluster-name\"")"
    region="$(apiclient get settings.aws.region | jq -r ".settings.aws.region")"
    apiclient set kubernetes.provider-id="eks-hybrid:///$region/$cluster_name/$hostname"

    # copy aws cred file from ssm control container to host for kubelet
    apiclient set aws.credentials="$(cat /.bottlerocket/rootfs/run/host-containerd/io.containerd.runtime.v2.task/default/control/rootfs/root/.aws/credentials | base64 -w0)"
fi

apiclient set host-containers.hybrid-setup.enabled=false
