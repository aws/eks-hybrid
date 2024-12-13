#!/usr/bin/env bash
# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
source $REPO_ROOT/hack/common.sh

DATE=date
if [ "$(uname -s)" = "Darwin" ]; then
    DATE=gdate
fi

TEST_CLUSTER_TAG_KEY="Nodeadm-E2E-Tests-Cluster"

CLUSTER_NAME=${1:-}

TIME_SINCE=${TIME_SINCE:-"1 day ago"}

TEST_CLUSTER_TAG_KEY_FILTER="Name=tag-key,Values=$TEST_CLUSTER_TAG_KEY"
if [ -n "$CLUSTER_NAME" ]; then
    TEST_CLUSTER_TAG_KEY_FILTER+=" Name=tag:$TEST_CLUSTER_TAG_KEY,Values=$CLUSTER_NAME"
fi


function aws() {
    retry build::common::echo_and_run command aws "$@"
}

function older_than_one_day(){
    created="$1"

    createDate=$($DATE -d"$created" +%s)
    olderThan=$($DATE --date "$TIME_SINCE" +%s)
    if [[ $createDate -lt $olderThan ]]; then
        return 0 # 0 = true
    else
        return 1 # 1 = false
    fi
}

declare -A CLUSTER_STATUSES=()
function is_eks_cluster_deleted(){
    name="$1"

    # +1 means to return 1 if the key exists, otherwise return nothing
    if [ ! ${CLUSTER_STATUSES[$name]+1} ]; then
        cluster_status="$(command aws eks describe-cluster --name $name --query 'cluster.status' --output text 2>/dev/null)"
        if [ $? != 0 ]; then
            # 1 = false
            CLUSTER_STATUSES[$name]="DELETING"
        else
            CLUSTER_STATUSES[$name]=$cluster_status
        fi
    fi
    if [ "DELETING" == ${CLUSTER_STATUSES[$name]} ]; then
        # 0 = true
        return 0
    else
        # 1 = false
        return 1
    fi
}

function get_cluster_name_from_tags(){
    json="$1"

    if [ "null" == "$(echo $json | jq ".Tags")" ]; then
        echo ""
        return
    fi

    cluster_name=$(echo $json | jq -r ".Tags | map(select(.Key == \"$TEST_CLUSTER_TAG_KEY\"))[0].Value")
    if [ -z "$cluster_name" ] || [ "$cluster_name" == "null" ]; then
        # the iam roles anywhere api returns tags where key/value are lower case, whereas the other apis all start with a upper...
        cluster_name=$(echo $json | jq -r ".Tags | map(select(.key == \"$TEST_CLUSTER_TAG_KEY\"))[0].value")       
    fi
    
    if [ -z "$cluster_name" ] || [ "$cluster_name" == "null" ]; then
        echo ""
        return
    fi
    
    echo $cluster_name
}

# Some resources like tags require a second api call to retrieve tags, or other data
# for these cases, we do not retry the request because if the cleanup is potentially running twice
# or tests are finishing up and deleting resources, we have seen cases where the tag will be deleted
# before we try to make the get tags call
function role_cluster_name_tag(){
    role="$1"
    role_tags="$(command aws iam list-role-tags --query "{Tags:Tags}" --role-name $role --output json 2>/dev/null)"
    if [ $? != 0 ]; then
        echo ""
        return        
    fi
    cluster_name="$(get_cluster_name_from_tags "$role_tags")"
    echo "$cluster_name"
}

# See above note about role tags
function instance_profile_cluster_name_tag(){
    instance_profile="$1"
    instance_profile_tags="$(command aws iam get-instance-profile --query "InstanceProfile.{Tags:Tags}" --instance-profile-name=$instance_profile --output json 2>/dev/null)"
    if [ $? != 0 ]; then
        echo ""
        return        
    fi
    cluster_name="$(get_cluster_name_from_tags "$instance_profile_tags")"
    echo "$cluster_name"
}

# For stack deletion we loop checking the status because in some cases
# we have to rerequest the delete with the force option
# we do not retry this call because we expect sometimes for it to come back empty
function describe_stack(){
    stack_name="$1"
    
    stack=$(command aws cloudformation describe-stacks --stack-name $stack_name --query "Stacks[*].{StackId:StackId,CreationTime:CreationTime,StackName:StackName,StackStatus:StackStatus,Tags:Tags}" --output json 2>/dev/null)
    if [ $? != 0 ]; then
        stack=""
    fi
    echo "$stack"
}

function delete_cluster(){
    eks_cluster="$1"

     aws eks delete-cluster --name $eks_cluster
}

function delete_instance(){
    instance_id="$1"
    cluster_name="$2"

    aws ec2 terminate-instances --instance-ids $instance_id
    aws ec2 wait instance-terminated --instance-ids $instance_id
}

function delete_peering_connection(){
    peering_connection_id="$1"
    cluster_name="$2"

    aws ec2 delete-vpc-peering-connection --vpc-peering-connection-id $peering_connection_id
    aws ec2 wait vpc-peering-connection-deleted --vpc-peering-connection-id $peering_connection_id
}

# Before deleting a role, it needs 
# - to be removed from all instance profiles it is attached to
# - attached polices need to be removed
# - polices need to be removed from it
function delete_role(){
    role="$1"

    for instance_profile in $(aws iam list-instance-profiles-for-role --role-name $role --query "InstanceProfiles[*].InstanceProfileName" --output text); do
        aws iam remove-role-from-instance-profile --instance-profile-name $instance_profile --role-name $role
    done

    while : ; do
        policy=$(aws iam list-attached-role-policies --role-name $role --query "AttachedPolicies[0].PolicyArn" --output text)
        if [ -z "$policy" ] || [ "$policy" == "None" ]; then
            break
        fi
        aws iam detach-role-policy --role-name $role --policy-arn $policy
    done

    for policy in $(aws iam list-role-policies --role-name $role --query "PolicyNames[*]" --output text); do
        aws iam delete-role-policy --role-name $role --policy-name $policy
    done

    aws iam delete-role --role-name $role
}

# Deleting vpcs requiring removing the igw, subnets and routes
# Some of these resources take time to delete and these requests are all retired to handle this
function delete_vpc(){
    vpc="$1"
    cluster_name="$2"

    for internet_gateway in $(aws ec2 describe-internet-gateways --filters "Name=attachment.vpc-id,Values=$vpc" --query "InternetGateways[*].InternetGatewayId"  --output text); do
        aws ec2 detach-internet-gateway --internet-gateway-id $internet_gateway --vpc-id $vpc
        aws ec2 delete-internet-gateway --internet-gateway-id $internet_gateway
    done

    for subnet in $(aws ec2 describe-subnets --filters "Name=vpc-id,Values=$vpc" --query "Subnets[*].SubnetId" --output text); do
        aws ec2 delete-subnet --subnet-id $subnet
    done

    main_route_table=$(aws ec2 describe-route-tables --filters "Name=vpc-id,Values=$vpc" "Name=association.main,Values=true" --query "RouteTables[*].RouteTableId" --output text)
    for rt in $(aws ec2 describe-route-tables --filters "Name=vpc-id,Values=$vpc" --query "RouteTables[*].RouteTableId" --output text); do
        if [[ "$rt" != "$main_route_table" ]]; then
            aws ec2 delete-route-table --route-table-id $rt
        fi
    done

    for sg in $(aws ec2 describe-security-groups --filters "Name=vpc-id,Values=$vpc" --query "SecurityGroups[*].{GroupId:GroupId,GroupName:GroupName}" --output json | jq -c '.[]'); do
        name=$(echo $sg | jq -r ".GroupName")
        if [[ "$name" != "default" ]]; then
            id=$(echo $sg | jq -r ".GroupId")
            aws ec2 delete-security-group --group-id $id
        fi
    done

    aws ec2 delete-vpc --vpc-id $vpc
}

# When a cluster name is supplied it along with all its resources are deleted
# No other resources are deleted
# We force the cluster status to be deleting in this case so that all future resources
# are deleted
if [ -n "$CLUSTER_NAME" ]; then
    if ! is_eks_cluster_deleted $CLUSTER_NAME; then
        delete_cluster $CLUSTER_NAME
    fi
    CLUSTER_STATUSES[$CLUSTER_NAME]="DELETING"
else
    # list clusters does not support tag filters so we pull all clusters
    # then describe to get the tags to filter out the ones that arent e2e tests clusters
    for eks_cluster in $(aws eks list-clusters --query "clusters" --output text); do
        if is_eks_cluster_deleted $eks_cluster; then
            continue
        fi
        describe=$(aws eks describe-cluster --name $eks_cluster --query 'cluster.{tags:tags,createdAt:createdAt}')
        if older_than_one_day "$(echo $describe | jq -r ".createdAt")"; then
            if [ "true" == $(echo $describe | jq ".tags | has(\"$TEST_CLUSTER_TAG_KEY\")") ]; then
                delete_cluster $eks_cluster
                CLUSTER_STATUSES[$eks_cluster]="DELETING"
            fi
        fi
    done
fi

# list-roles does not allow filtering by tags so we have to pull them all
# then request their tags seperately
# We have the role =* checks to try and limit which roles we bother checking tags for
# but we only delete those with the e2e cluster tag
# If the cluster-name is passed to the script, only roles who's tag matches that cluster
# are deleted
for role in $(aws iam list-roles --query 'Roles[*].RoleName' --output text); do
    if [[ $role == *-hybrid-node ]] || [[ $role == nodeadm-e2e-tests* ]] || [[ $role == EKSHybridCI-* ]]; then
        cluster_name="$(role_cluster_name_tag $role)"
        if [ -n "$CLUSTER_NAME" ] && [ "$cluster_name" != "$CLUSTER_NAME" ]; then
            continue
        fi
        if [ -z "$cluster_name" ] || ! is_eks_cluster_deleted $cluster_name; then
            continue
        fi
      
        delete_role $role        
    fi
done

# See note above about roles
for instance_profile in $(aws iam list-instance-profiles  --query 'InstanceProfiles[*].InstanceProfileName' --output text); do
    if [[ $instance_profile == EKSHybridCI-* ]]; then
        cluster_name="$(instance_profile_cluster_name_tag $instance_profile)"
        if [ -n "$CLUSTER_NAME" ] && [ "$cluster_name" != "$CLUSTER_NAME" ]; then
            continue
        fi
        if [ -z "$cluster_name" ] || ! is_eks_cluster_deleted $cluster_name; then
            continue
        fi
        aws iam delete-instance-profile --instance-profile-name $instance_profile        
    fi
done

# describe-stacks does not allow filter but it does return the tags for each stack
# This deletion is retried since in some cases it has to be remade with the force flag
# If the cluster-name is passed to the script, only stacks who's tag matches that cluster
# are deleted
for stack in $(aws cloudformation describe-stacks --query "Stacks[*].{StackId:StackId,CreationTime:CreationTime,StackName:StackName,StackStatus:StackStatus,Tags:Tags}" --output json | jq -c '.[]'); do
    while : ; do       
        stack_name=$(echo $stack | jq -r ".StackName")
        if [[ $stack_name != EKSHybridCI-* ]]; then
            break
        fi
        cluster_name="$(get_cluster_name_from_tags "$stack")"
        if [ -n "$CLUSTER_NAME" ] && [ "$cluster_name" != "$CLUSTER_NAME" ]; then
            break
        fi
        created=$(echo $stack | jq -r ".CreationTime")
        if [ -z "$CLUSTER_NAME" ] && ! older_than_one_day $created; then
            break
        fi
        status=$(echo $stack | jq -r ".StackStatus")
        deletion_mode="STANDARD"
        if [ "$status" == "DELETE_FAILED" ]; then
            deletion_mode="FORCE_DELETE_STACK"
        fi
        stack_name=$(echo $stack | jq -r ".StackName")
        if [ "$status" != "DELETE_IN_PROGRESS" ]; then
            aws cloudformation delete-stack --stack-name $stack_name --deletion-mode $deletion_mode
        fi
        sleep 5
        stack=$(describe_stack $stack_name)
        if [[ -n "$stack" ]]; then
            stack="$(echo $stack | jq -c '.[]')"
        else
            break
        fi
    done
done

# skip these in ap-southeast-5
if [[ "${SKIP_IRA_TEST:-false}" == "false" ]]; then
    # See note above about roles
    # these rolesanywhere profiles should be deleted when deleting the above cfn stacks
    # this is a fallback
    for profile in $(aws rolesanywhere list-profiles --query 'profiles[*].{name:name,profileId:profileId,profileArn:profileArn}' --output json | jq -c '.[]'); do
        name=$(echo "$profile" | jq -r ".name")
        if [[ $name == EKSHybridCI-* ]]; then
            arn=$(echo "$profile" | jq -r ".profileArn")
            profile_tags="$(aws rolesanywhere list-tags-for-resource --resource-arn $arn --query "{Tags:tags}" --output json)"
            cluster_name="$(get_cluster_name_from_tags "$profile_tags")"       
            if [ -n "$CLUSTER_NAME" ] && [ "$cluster_name" != "$CLUSTER_NAME" ]; then
                continue
            fi
            if [ -z "$cluster_name" ] || ! is_eks_cluster_deleted $cluster_name; then
                continue
            fi
            id=$(echo "$profile" | jq -r ".profileId")
            aws rolesanywhere delete-profile --profile-id $id     
        fi
    done

    # See note above about roles
    # these rolesanywhere profiles should be deleted when deleting the above cfn stacks
    # this is a fallback
    for anchor in $(aws rolesanywhere list-trust-anchors --query 'trustAnchors[*].{name:name,trustAnchorId:trustAnchorId,trustAnchorArn:trustAnchorArn}' --output json | jq -c '.[]'); do
        name=$(echo "$anchor" | jq -r ".name")
        if [[ $name == EKSHybridCI-* ]]; then
            arn=$(echo "$anchor" | jq -r ".trustAnchorArn")
            anchor_tags="$(aws rolesanywhere list-tags-for-resource --resource-arn $arn --query "{Tags:tags}" --output json)"
            cluster_name="$(get_cluster_name_from_tags "$anchor_tags")"       
            if [ -n "$CLUSTER_NAME" ] && [ "$cluster_name" != "$CLUSTER_NAME" ]; then
                continue
            fi
            if [ -z "$cluster_name" ] || ! is_eks_cluster_deleted $cluster_name; then
                continue
            fi
            id=$(echo "$anchor" | jq -r ".trustAnchorId")
            aws rolesanywhere delete-trust-anchor --trust-anchor-id $id     
        fi
    done
fi

# The passed in filters will only return instances with the e2e cluster tag
# If a cluster name was passed to the script, it also added to the filters so that we
# only are getting instances associated with that cluster
for reservations in $(aws ec2 describe-instances --filters $TEST_CLUSTER_TAG_KEY_FILTER --query "Reservations[*].Instances[*].{InstanceId:InstanceId,Tags:Tags,State:State.Name}" --output json | jq -c '.[]'); do
    for ec2 in $(echo $reservations | jq -c '.[]'); do
        if [ "terminated" == "$(echo "$ec2" | jq -r '.State')" ]; then
            continue
        fi
        cluster_name="$(get_cluster_name_from_tags "$ec2")"
        instance_id=$(echo $ec2 | jq -r ".InstanceId")
        if ! is_eks_cluster_deleted $cluster_name; then
            continue
        fi
        delete_instance $instance_id $cluster_name
    done
done

# # See above note about instances
for peering_connection in $(aws ec2 describe-vpc-peering-connections --filters $TEST_CLUSTER_TAG_KEY_FILTER --query "VpcPeeringConnections[*].{VpcPeeringConnectionId:VpcPeeringConnectionId,Tags:Tags,StatusCode:Status.Code}" --output json | jq -c '.[]'); do
    if [ "deleted" == "$(echo "$peering_connection" | jq -r '.StatusCode')" ]; then
        continue
    fi
    cluster_name="$(get_cluster_name_from_tags "$peering_connection")"
    peering_connection_id=$(echo "$peering_connection" | jq -r ".VpcPeeringConnectionId")
    if ! is_eks_cluster_deleted $cluster_name; then
        continue
    fi
    delete_peering_connection $peering_connection_id $cluster_name
done

# # See above note about vpcs
for vpc in $(aws ec2 describe-vpcs --filters $TEST_CLUSTER_TAG_KEY_FILTER --query "Vpcs[*].{VpcId:VpcId,Tags:Tags}"| jq -c '.[]'); do
    cluster_name="$(get_cluster_name_from_tags "$vpc")"
    vpc=$(echo $vpc | jq -r ".VpcId")
    if ! is_eks_cluster_deleted $cluster_name; then
        continue
    fi
    delete_vpc $vpc $cluster_name
done

# these should get detached and deleted during vpc cleanup
# but the detach and delete are seperate calls so catch
# any lingering resources
for internet_gateway in $(aws ec2 describe-internet-gateways --filters $TEST_CLUSTER_TAG_KEY_FILTER --query "InternetGateways[*].{InternetGatewayId:InternetGatewayId,Tags:Tags}" | jq -c '.[]'); do
    cluster_name="$(get_cluster_name_from_tags "$internet_gateway")"
    if [ -z "$cluster_name" ]; then
        continue
    fi
    if [ -n "$CLUSTER_NAME" ] && [ "$cluster_name" != "$CLUSTER_NAME" ]; then
        continue
    fi
    if ! is_eks_cluster_deleted $cluster_name; then
        continue
    fi
    id=$(echo $internet_gateway | jq -r ".InternetGatewayId")
    aws ec2 delete-internet-gateway --internet-gateway-id $id
done

# describe-activations does not allow filters but does return tags
# If the cluster-name is passed to the script, activations stacks who's tag matches that cluster
# are deleted
# Before deleting the activation, the associated managed instances are also deleted
for activation in $(aws ssm describe-activations --query "ActivationList[*].{ActivationId:ActivationId,Tags:Tags}" --output json | jq -c '.[]'); do
    cluster_name="$(get_cluster_name_from_tags "$activation")"
    if [ -z "$cluster_name" ]; then
        continue
    fi
    if [ -n "$CLUSTER_NAME" ] && [ "$cluster_name" != "$CLUSTER_NAME" ]; then
        continue
    fi
    if ! is_eks_cluster_deleted $cluster_name; then
        continue
    fi
    id=$(echo $activation | jq -r ".ActivationId")
    for managed_instance_id in $(aws ssm describe-instance-information --filters "Key=ActivationIds,Values=$id" --query "InstanceInformationList[*].InstanceId" --output text); do
        aws ssm deregister-managed-instance --instance-id $managed_instance_id
    done
  
    aws ssm delete-activation --activation-id $id
done

# describe-instance-information allows filters and we filter only those with the e2e cluster tag
# If a cluster name is passed to the script, it is also added to the filters
describe_instance_filters="Key=tag-key,Values=$TEST_CLUSTER_TAG_KEY"
if [ -n "$CLUSTER_NAME" ]; then
    describe_instance_filters="Key=tag:$TEST_CLUSTER_TAG_KEY,Values=$CLUSTER_NAME"
fi

for managed_instance in $(aws ssm describe-instance-information --max-items 100 --filters "$describe_instance_filters" --query "InstanceInformationList[*].{InstanceId:InstanceId,LastPingDateTime:LastPingDateTime,ResourceType:ResourceType}" --output json | jq -c '.[]'); do
    resource_type=$(echo $managed_instance | jq -r ".ResourceType")
    if [ "$resource_type" != "ManagedInstance" ]; then  
        continue
    fi
    last_ping=$(echo $managed_instance | jq -r ".LastPingDateTime")
    if [ -z "$CLUSTER_NAME" ] && ! older_than_one_day $last_ping; then
        continue
    fi
    id=$(echo $managed_instance | jq -r ".InstanceId")
    aws ssm deregister-managed-instance --instance-id $id
done
