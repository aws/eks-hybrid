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

# Short-circuit if script has already been sourced
[[ $(type -t build::common::loaded) == function ]] && return 0

function build::common::echo_and_run() {
    >&2 echo "($(pwd)) \$ $*"
    "$@"
}

function fail() {
  echo $1 >&2
  exit 1
}

function retry() {
    local n=1
    local max=40
    local delay=10
    while true; do
        "$@" && break || {
            if [[ $n -lt $max ]]; then
                ((n++))
                >&2 echo "Command failed. Attempt $n/$max:"
                sleep $delay;
            else
                fail "The command has failed after $n attempts."
            fi
        }
    done
}

# Marker function to indicate script has been fully sourced
function build::common::loaded() {
  return 0
}

function build::common::generate_output_logs() {
    local -r report_file=$1

    build::common::jq_update_in_place $report_file 'del(.[0].SpecReports[] | select(.CapturedStdOutErr == null))'
    build::common::generate_output_log_files $report_file "test-cases"
    build::common::generate_output_log_files $report_file "setup"
    build::common::generate_output_log_files $report_file "cleanup"
}

function build::common::leaf_node_type_for_report_type() {
    local -r report_type=$1

    case "$report_type" in
	"setup")
	    echo "SynchronizedBeforeSuite"
	    ;;
	"cleanup")
	    echo "DeferCleanup (Suite)"
	    ;;
	"test-cases")
	    echo "It"
	    ;;
	*)
	    echo "Unrecognized report type '$1'" 1>&2
	    exit 1
	    ;;
    esac
}

function build::common::jq_update_in_place() {
  local -r json_file=$1
  local -r jq_query=$2

  cat $json_file | jq -S ''"$jq_query"'' > $json_file.tmp && mv $json_file.tmp $json_file
}

function build::common::generate_output_log_files() {
    local -r report_file=$1
    local -r report_type=$2

    local -r reports_directory=$(dirname $report_file)
    local -r filtered_report_file=$reports_directory/$report_type.json
    local -r leaf_node_type=$(build::common::leaf_node_type_for_report_type $report_type)

    cat $report_file | jq --arg leaf_node_type "$leaf_node_type" 'del(.[0].SpecReports[] | select(.LeafNodeType != $leaf_node_type))' > $filtered_report_file

    num_nested_reports=$(cat $filtered_report_file | jq '.[0].SpecReports | length')
    if [ $num_nested_reports -gt 0 ]; then
        for index in $(seq 0 $(($num_nested_reports-1))); do
            log_file_name=""
            if [[ $report_type == "setup" || $report_type == "cleanup" ]]; then
                log_file_name=$report_type.log
            else
                labels=($(cat $filtered_report_file | jq -r --arg index $index '.[0].SpecReports[$index|tonumber].LeafNodeLabels[]'))
                log_file_name=$(IFS='-'; echo "${labels[*]}").log
            fi
            cat $filtered_report_file | jq --arg index $index '.[0].SpecReports[$index|tonumber] | .CapturedStdOutErr' | sed -e 's/\\u001b\[34mINFO\\u001b\[0m/INFO/g' -e 's/\\t/\t/g' -e 's/\\n/\n/g' -e 's/\\"/\"/g' > $reports_directory/$log_file_name
        done
    fi
    rm $filtered_report_file
}
