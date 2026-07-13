#!/usr/bin/env bash
# get_volumes_info.sh — Retrieve AWS EBS volume configuration for the current EC2 instance
# Outputs a JSON object with instance metadata and volume attachment details.
# Intended for use with command-runner or other monitoring tools.

set -euo pipefail

# ── IMDSv2 token ────────────────────────────────────────────────────────────
TOKEN=$(curl -s -f -X PUT "http://169.254.169.254/latest/api/token" \
  -H "X-aws-ec2-metadata-token-ttl-seconds: 21600") || {
  echo '{"error": "Failed to obtain IMDSv2 token. Is this an EC2 instance?"}' >&2
  exit 1
}

imds() {
  curl -s -f -H "X-aws-ec2-metadata-token: $TOKEN" \
    "http://169.254.169.254/latest/meta-data/$1"
}

# ── Check AWS CLI version ────────────────────────────────────────────────────
check_aws_cli_version() {
  local version_output=""
  local major_version=""
  local arch=""
  local pkg_arch=""
  local zip_url=""
  local install_dir="/tmp/awscliv2-install"

  version_output=$(aws --version 2>&1 || true)
  major_version=$(echo "$version_output" | sed -n 's/^aws-cli\/\([0-9]\+\)\..*/\1/p')

  if [[ -n "$major_version" ]] && [[ "$major_version" -ge 2 ]]; then
    return 0
  fi

  echo "INFO: AWS CLI v2 required for EBS Throughput data; installing/upgrading now" >&2

  if [[ $(id -u) -ne 0 ]]; then
    echo "ERROR: AWS CLI v2 installation requires root privileges (run as root/sudo)" >&2
    exit 1
  fi

  if command -v yum >/dev/null 2>&1; then
    yum remove awscli -y >/dev/null 2>&1 || true
    yum install -y unzip curl >/dev/null
  elif command -v dnf >/dev/null 2>&1; then
    dnf remove awscli -y >/dev/null 2>&1 || true
    dnf install -y unzip curl >/dev/null
  elif command -v apt-get >/dev/null 2>&1; then
    apt-get update -y >/dev/null
    apt-get remove -y awscli >/dev/null 2>&1 || true
    apt-get install -y unzip curl >/dev/null
  else
    echo "ERROR: Unsupported package manager for automatic AWS CLI installation" >&2
    exit 1
  fi

  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) pkg_arch="x86_64" ;;
    aarch64|arm64) pkg_arch="aarch64" ;;
    *)
      echo "ERROR: Unsupported architecture for AWS CLI v2 install: $arch" >&2
      exit 1
      ;;
  esac

  zip_url="https://awscli.amazonaws.com/awscli-exe-linux-${pkg_arch}.zip"

  rm -rf "$install_dir"
  mkdir -p "$install_dir"
  cd "$install_dir" || {
    echo "ERROR: Failed to cd to install dir: $install_dir" >&2
    exit 1
  }

  curl -fsSL "$zip_url" -o awscliv2.zip || {
    echo "ERROR: Failed to download AWS CLI v2 from $zip_url" >&2
    exit 1
  }
  unzip -q -o awscliv2.zip || {
    echo "ERROR: Failed to unzip awscliv2.zip" >&2
    exit 1
  }

  if [[ -x /usr/local/bin/aws ]]; then
    ./aws/install --update || {
      echo "ERROR: Failed to update AWS CLI v2" >&2
      exit 1
    }
  else
    ./aws/install || {
      echo "ERROR: Failed to install AWS CLI v2" >&2
      exit 1
    }
  fi

  version_output=$(aws --version 2>&1 || true)
  major_version=$(echo "$version_output" | sed -n 's/^aws-cli\/\([0-9]\+\)\..*/\1/p')
  if [[ -z "$major_version" ]] || [[ "$major_version" -lt 2 ]]; then
    echo "ERROR: AWS CLI install attempted but v2 is not active. Current version output: ${version_output:-unknown}" >&2
    exit 1
  fi

  echo "INFO: AWS CLI successfully installed/upgraded: $version_output" >&2
}

check_aws_cli_version

# ── Instance metadata ────────────────────────────────────────────────────────
INSTANCE_ID=$(imds instance-id)
REGION=$(imds placement/region)
INSTANCE_TYPE=$(imds instance-type)
AZ=$(imds placement/availability-zone)

# ── Volume details via AWS CLI ───────────────────────────────────────────────
VOLUMES=$(aws ec2 describe-volumes \
  --region "$REGION" \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" \
  --query 'Volumes[*].{
    VolumeId:       VolumeId,
    Device:         Attachments[0].Device,
    VolumeType:     VolumeType,
    SizeGiB:        Size,
    IOPS:           Iops,
    ThroughputMBps: Throughput,
    Encrypted:      Encrypted,
    KmsKeyId:       KmsKeyId,
    State:          State,
    MultiAttach:    MultiAttachEnabled,
    SnapshotId:     SnapshotId
  }' \
  --output json)

# ── Block device / filesystem / mount info ───────────────────────────────────
LSBLK_JSON=$(lsblk --json --output NAME,TYPE,FSTYPE,MOUNTPOINT,SIZE,UUID \
  --paths 2>/dev/null || echo '{"blockdevices":[]}')

# Parse /proc/mounts for accurate mount options, emit as a JSON array.
MOUNTS_JSON=$(awk '
  /^\/dev\// {
    gsub(/"/,"\\\"");
    printf "{\"device\":\"%s\",\"mountpoint\":\"%s\",\"fstype\":\"%s\",\"options\":\"%s\"},\n",
      $1, $2, $3, $4
  }
' /proc/mounts | sed '$ s/,$//' | { echo "["; cat; echo "]"; })

# ── Correlate AWS VolumeId → OS device via /dev/disk/by-id/ ─────────────────
# On Nitro/NVMe instances the AWS attachment name (/dev/sdb) differs from the
# OS device (/dev/nvme1n1). The kernel creates symlinks of the form:
#   nvme-Amazon_Elastic_Block_Store_vol<id-without-dash>  ->  ../../nvmeXn1
# We read those to build a definitive VolumeId → OS device map.
VOL_MAP='{}'
if [[ -d /dev/disk/by-id ]]; then
  while IFS= read -r name; do
    if [[ "$name" =~ ^nvme-Amazon_Elastic_Block_Store_(vol[0-9a-f]+)$ ]]; then
      vol_raw="${BASH_REMATCH[1]}"          # vol027598fbe3893ccec
      vol_id="vol-${vol_raw#vol}"           # vol-027598fbe3893ccec
      os_dev=$(readlink -f "/dev/disk/by-id/$name")  # /dev/nvme1n1
      VOL_MAP=$(jq -n --argjson m "$VOL_MAP" \
        --arg vid "$vol_id" --arg dev "$os_dev" \
        '$m + {($vid): $dev}')
    fi
  done < <(ls /dev/disk/by-id/ 2>/dev/null \
    | grep '^nvme-Amazon_Elastic_Block_Store_vol' \
    | grep -v -- '-part[0-9]')
fi

# ── Combine and correlate into a single JSON document ───────────────────────
jq -n \
  --arg instance_id    "$INSTANCE_ID" \
  --arg region         "$REGION" \
  --arg instance_type  "$INSTANCE_TYPE" \
  --arg az             "$AZ" \
  --argjson volumes    "$VOLUMES" \
  --argjson lsblk      "$LSBLK_JSON" \
  --argjson mounts     "$MOUNTS_JSON" \
  --argjson vol_map    "$VOL_MAP" \
  '
  # Convert AWS attachment device to likely Linux device path on Xen-based hosts.
  # Examples: /dev/sdb -> /dev/xvdb, /dev/sda1 -> /dev/xvda1
  def sd_to_xvd($d):
    if ($d // "") | startswith("/dev/sd") then
      "/dev/xvd" + (($d | sub("^/dev/sd"; "")) // "")
    else
      $d
    end;

  # Flatten block devices one level (disk + partitions) keyed by device name
  [ $lsblk.blockdevices[] | ., (.children // [])[] ] as $all_blk |
  ($all_blk | map({key: .name, value: .}) | from_entries) as $blk_by_dev |

  # Mount lookup keyed by device name
  ($mounts | map({key: .device, value: .}) | from_entries) as $mount_by_dev |

  # Enrich each EBS volume with its OS device name and mount info
  [
    $volumes[] |
    . as $vol |
    # Prefer explicit NVMe volume-id mapping if available, otherwise derive from AWS device name.
    (($vol_map[$vol.VolumeId]) // (sd_to_xvd($vol.Device))) as $os_dev |
    # Prefer a direct mount; fall back to first mounted partition
    (
      if ($os_dev != null and $mount_by_dev[$os_dev] != null) then
        $mount_by_dev[$os_dev]
      else
        [ (($blk_by_dev[$os_dev].children // [])[]? | .name) as $child_name |
          $mount_by_dev[$child_name] | select(. != null) ]
        | (map(select(.mountpoint == "/")) | .[0]) // .[0]
      end
    ) as $mnt |
    $vol + {
      os_device:     $os_dev,
      mountpoint:    ($mnt.mountpoint    // null),
      fstype:        ($mnt.fstype        // null),
      mount_options: ($mnt.options       // null)
    }
  ] as $enriched_volumes |

  {
    instance_id:       $instance_id,
    region:            $region,
    instance_type:     $instance_type,
    availability_zone: $az,
    volumes:           $enriched_volumes,
    block_devices:     $lsblk.blockdevices,
    mounts:            $mounts
  }'
