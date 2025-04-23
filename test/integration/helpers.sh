#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

function assert::files-equal() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::files-equal FILE1 FILE2"
    exit 1
  fi
  local FILE1=$1
  local FILE2=$2
  if ! diff $FILE1 $FILE2; then
    echo "Files $FILE1 and $FILE2 are not equal"
    exit 1
  fi
}

function assert::files-not-equal() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::files-equal FILE1 FILE2"
    exit 1
  fi
  local FILE1=$1
  local FILE2=$2
  if diff $FILE1 $FILE2; then
    echo "Files $FILE1 and $FILE2 are equal"
    exit 1
  fi
}

function assert::json-files-equal() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::json-files-equal FILE1 FILE2"
    exit 1
  fi
  local FILE1=$1
  stat $FILE1
  local FILE2=$2
  stat $FILE2
  if ! diff <(jq -S . $FILE1) <(jq -S . $FILE2); then
    echo "Files $FILE1 and $FILE2 are not equal"
    echo "**** Actual Content ****"
    cat $FILE1
    echo "**************************"
    echo "**** Expected Content ****"
    cat $FILE2
    echo "**************************"
    exit 1
  fi
}

function assert::file-contains() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::file-contains FILE PATTERN"
    exit 1
  fi
  local FILE=$1
  local PATTERN=$2
  if ! grep -e "$PATTERN" $FILE; then
    echo "File $FILE does not contain pattern '$PATTERN'"
    cat $FILE
    echo ""
    exit 1
  fi
}

function assert::path-exists() {
  if [ "$#" -ne 1 ]; then
    echo "Usage: assert::path-exists INPUT_PATH"
    exit 1
  fi
  local INPUT_PATH=$1
  if ! [[ -e "$INPUT_PATH" ]]; then
      echo "Path $INPUT_PATH does not exist"
      exit 1
  fi
}

function assert::path-not-exist() {
  if [ "$#" -ne 1 ]; then
    echo "Usage: assert::path-not-exist INPUT_PATH"
    exit 1
  fi
  local INPUT_PATH=$1
  if [ -e "$INPUT_PATH" ]; then
      echo "Path $INPUT_PATH exists!"
      exit 1
  fi
}

function assert::file-not-contains() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::file-not-contains FILE PATTERN"
    exit 1
  fi
  local FILE=$1
  local PATTERN=$2
  if grep -e "$PATTERN" $FILE; then
    echo "File $FILE contains pattern '$PATTERN'"
    cat $FILE
    echo ""
    exit 1
  fi
}

function generate::birth-file() {
    if [ "$#" -ne 1 ]; then
      echo "Usage: generate::stat-file INPUT_PATH"
      exit 1
    fi
    local INPUT_PATH=$1
    rm -rf INPUT_PATH.stat
    echo $(stat -c %W $INPUT_PATH) > $INPUT_PATH.birth
}

function assert::birth-match() {
  if [ "$#" -ne 1 ]; then
    echo "Usage: assert::stat-match INPUT_PATH"
    exit 1
  fi
  local INPUT_PATH=$1
  echo $(stat -c %W $INPUT_PATH) > $INPUT_PATH.current.birth
  assert::files-equal $INPUT_PATH.birth $INPUT_PATH.current.birth
}

function assert::birth-not-match() {
  if [ "$#" -ne 1 ]; then
    echo "Usage: assert::stat-match INPUT_PATH"
    exit 1
  fi
  local INPUT_PATH=$1
  echo $(stat $INPUT_PATH) > $INPUT_PATH.current.stat
  assert::files-not-equal $INPUT_PATH.stat $INPUT_PATH.current.stat
}

function assert::is-substring() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::is-substring STRING PATTERN"
    exit 1
  fi
  local STRING=$1
  local PATTERN=$2
  if ! [[ $STRING == *"$PATTERN"* ]]; then
    echo "$STRING does not contain substring $PATTERN"
    exit 1
  fi
}

function assert::swap-disabled() {
  if [ "$#" -ne 0 ]; then
    echo "Usage: assert::swap-disabled"
    exit 1
  fi
  if [[ $(swapon --show) ]]; then
    echo "Swap is not disabled!"
    exit 1
  fi
}

function assert::swap-disabled-validate-path() {
  if [ "$#" -ne 0 ]; then
    echo "Usage: assert::swap-disabled-validate-path"
    exit 1
  fi
  # read /proc/swaps file and skip the first line, check if swapfile path still exists,
  # (1) If swap is disabled, then swap will not appear in /proc/swaps
  # (2) If the filepath of a swap does not exist, then the swap invalid, and will be treated as if it has been disabled
  tail -n +2 /proc/swaps | while IFS=$'\t' read -r filename type size used priority; do
    # echo "First Var is $key - Remaining Line Contents Are $value"
    if [ -e "$filename" ]; then
      echo "Swap is not disabled for swapfile $filename"
      exit 1
    fi
  done
}

function assert::allowed-by-firewalld() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::allowed-by-firewalld [port(range)] [protocol]"
    exit 1
  fi
  local PATTERN="$1/$2"
  if ! [[ $(firewall-cmd --list-ports | grep $PATTERN) ]]; then
    echo "Port $PATTERN is not allowed by firewalld!"
    exit 1
  fi
}

function assert::file-permission-matches() {
  if [ "$#" -ne 2 ]; then
    echo "Usage: assert::file-permission-matches [file path] [expected file permission in numberic, example: 644, 755]"
    exit 1
  fi
  local FILE_PATH=$1
  local EXPECTED_PERMISSION=$2
  local FILE_PERMISSION=$(stat -c %a $FILE_PATH)
  if ! [[ $FILE_PERMISSION == $EXPECTED_PERMISSION ]]; then
    echo "File $FILE_PATH's permission $FILE_PERMISSION does not match expected permission $EXPECTED_PERMISSION"
    exit 1
  fi
}

function assert::output-contains-ssm-url() {
    if [ "$#" -ne 2 ]; then
        echo "Usage: assert::output-contains-ssm-url [output] [region]"
        exit 1
    fi

    local output=$1
    local region=$2
    local arch=$(uname -m)

    # Convert architecture names to match SSM's format
    case "$arch" in
        "x86_64")  arch="linux_amd64" ;;
        "aarch64") arch="linux_arm64" ;;
        *) echo "Unsupported architecture: $arch" >&2; return 1 ;;
    esac

    local expected_url="https://amazon-ssm-${region}.s3.${region}.amazonaws.com/latest/${arch}/ssm-setup-cli"

    if ! echo "$output" | grep -q "$expected_url"; then
        echo "Output does not contain expected SSM URL: $expected_url"
        return 1
    fi
}

# Check if a non-json file exists and verify its permission, if a 3rd argument is provided, also check file content
function validate-file() {
  if [[ "$#" -ne 2 && "$#" -ne 3 ]]; then
    echo "Usage: assert::validate-file [file path] [expected file permission] [path of file with expected content (optional)]"
    exit 1
  fi
  local FILE_PATH=$1
  local EXPECTED_PERMISSION=$2
  assert::path-exists $FILE_PATH
  assert::file-permission-matches $FILE_PATH $EXPECTED_PERMISSION
  if [ "$#" -eq 3 ]; then
    local EXPECTED_CONTENT_FILE_PATH=$3
    assert::files-equal $FILE_PATH $EXPECTED_CONTENT_FILE_PATH
  fi
}

# Check if a json file exists and verify its permission, if a 3rd argument is provided, also check file content
function validate-json-file() {
  if [[ "$#" -ne 2 && "$#" -ne 3 ]]; then
    echo "Usage: assert::validate-json-file [json file path] [expected file permission] [path of file with expected content (optional)]"
    exit 1
  fi
  local FILE_PATH=$1
  local EXPECTED_PERMISSION=$2
  local EXPECTED_CONTENT_FILE_PATH=$3
  assert::path-exists $FILE_PATH
  assert::file-permission-matches $FILE_PATH $EXPECTED_PERMISSION
  if [ "$#" -eq 3 ]; then
    local EXPECTED_CONTENT_FILE_PATH=$3
    assert::json-files-equal $FILE_PATH $EXPECTED_CONTENT_FILE_PATH
  fi
}

function mock::kubelet() {
  if [ "$#" -ne 1 ]; then
    echo "Usage: mock::kubelet VERSION"
    exit 1
  fi
  printf "#!/usr/bin/env bash\necho Kubernetes v%s\n" "$1" > /usr/bin/kubelet
  chmod +x /usr/bin/kubelet
}

function mock::aws_signing_helper() {
  printf "#!/usr/bin/env bash\necho '{\"Version\": 1, \"AccessKeyId\": \"${AWS_ACCESS_KEY_ID}\", \"SecretAccessKey\": \"${AWS_SECRET_ACCESS_KEY}\", \"SessionToken\": \"${AWS_SESSION_TOKEN}\"}'" > /usr/local/bin/aws_signing_helper
  chmod +x /usr/local/bin/aws_signing_helper
}
function mock::iamra_aws_credentials() {
  mkdir -p /eks-hybrid/.aws
  touch /eks-hybrid/.aws/credentials
}

function mock::ssm() {
  # mock ssm agent binary
  if [ -e  /usr/bin/amazon-ssm-agent ]; then
    printf "#!/usr/bin/env bash\necho SSM" > /usr/bin/amazon-ssm-agent
    chmod +x /usr/bin/amazon-ssm-agent
  fi

  if [ -e  /snap/amazon-ssm-agent/current/amazon-ssm-agent ]; then
    printf "#!/usr/bin/env bash\necho SSM" > /snap/amazon-ssm-agent/current/amazon-ssm-agent"
    chmod +x /snap/amazon-ssm-agent/current/amazon-ssm-agent"
  fi

  # mock ssm registration
  cat > /var/lib/amazon/ssm/registration << EOF
{"ManagedInstanceID": "","Region": "$AWS_REGION"}
EOF

  # mock ssm credentials
  mkdir /root/.aws
  cat > /root/.aws/credentials << EOF
[default]
aws_access_key_id     = $AWS_ACCESS_KEY_ID
aws_secret_access_key = $AWS_SECRET_ACCESS_KEY
aws_session_token     = $(echo $AWS_SESSION_TOKEN | base64)
EOF

}

function mock::setup-local-disks() {
  mkdir -p /var/log
  printf '#!/usr/bin/env bash\necho "$1" >> /var/log/setup-local-disks.log' > /usr/bin/setup-local-disks
  chmod +x /usr/bin/setup-local-disks
}

function wait::path-exists() {
  if [ "$#" -ne 1 ]; then
    echo "Usage: wait::path-exists TARGET_PATH"
    return 1
  fi
  local TARGET_PATH=$1
  local TIMEOUT=10
  local INTERVAL=1
  local ELAPSED=0
  while ! stat $TARGET_PATH; do
    if [ $ELAPSED -ge $TIMEOUT ]; then
      echo "Timed out waiting for $TARGET_PATH"
      return 1
    fi
    sleep $INTERVAL
    ELAPSED=$((ELAPSED + INTERVAL))
  done
}

function wait::dbus-ready() {
  wait::path-exists /run/systemd/private
}

# run_in_background run a command in the background and wait for a specified period
# If the command is still running after the wait period, assume it will not fail and continue
# If the command has finished, check its exit status
# Return 0 if the command finished successfully within the wait period, otherwise return the exit status
function run_in_background() {
    local command="$1"
    local wait_time=${2:-1} # Default wait time is 1 second

    # Run the command in the background
    eval "$command &"
    local pid=$!

    # Wait for the specified period
    sleep "$wait_time"

    # Check if the process is still running
    if kill -0 "$pid" 2>/dev/null; then
        echo "Command [${command}] is still running, assuming it won't fail, continuing..."
        return 0
    else
        # Process has finished; check its exit status
        wait "$pid"
        local status=$?
        if [ $status -ne 0 ]; then
            echo "Command [${command}] failed with exit status $status"
            return $status
        fi
        echo "Command [${command}] finished successfully within the wait period"
        return 0
    fi
}

function mock::aws() {
  local CONFIG_PATH=${1:-/etc/aemm-default-config.json}
  if [ "${IMDS_MOCK_ONLY_CONFIGURE:-}" != "true" ] ;then
    if [ ! -f "$CONFIG_PATH" ]; then
      echo "Config file $CONFIG_PATH does not exist"
      exit 1
    fi

    if ! run_in_background "imds-mock --config-file $CONFIG_PATH" 1; then
      echo "Setting up IMDS mock failed"
      exit 1
    fi
  fi

  export AWS_EC2_METADATA_SERVICE_ENDPOINT=http://localhost:1338
  [ "${AWS_MOCK_ONLY_CONFIGURE:-}" = "true" ] || $HOME/.local/bin/moto_server -p5000 &
  export AWS_ACCESS_KEY_ID='testing'
  export AWS_SECRET_ACCESS_KEY='testing'
  export AWS_SECURITY_TOKEN='testing'
  export AWS_SESSION_TOKEN='testing'
  export AWS_REGION=us-east-1
  export AWS_ENDPOINT_URL=http://localhost:5000
  # ensure that our instance exists in the API
  aws ec2 run-instances
}
