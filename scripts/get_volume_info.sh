#!/usr/bin/env bash
# get_volumes.sh — Retrieve EBS volume configuration for the current EC2 instance
# Outputs a JSON object with instance metadata and attached volume details.

set -euo pipefail

usage() {
  cat << 'EOF'
Usage:
  ./get_volume_info.sh [--instance-id i-xxxxxxxxxxxxxxxxx] [--region us-east-1]

Options:
  --instance-id ID    Query a specific instance (for CloudShell/local use)
  --region REGION     AWS region for API calls (optional if configured)
  -h, --help          Show this help

Notes:
  - Without --instance-id, metadata is read from IMDS on the current EC2 instance.
  - With --instance-id, the script uses AWS APIs and does not rely on IMDS.
EOF
}

die() {
  echo "Error: $*" >&2
  exit 1
}

INSTANCE_ID_ARG=""
REGION_ARG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance-id)
      INSTANCE_ID_ARG="${2:-}"
      shift 2
      ;;
    --region)
      REGION_ARG="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

# ── Check AWS CLI version ────────────────────────────────────────────────────
check_aws_cli_version() {
  local version_output
  version_output=$(aws --version 2>&1 || echo "aws-cli/1.0.0")
  
  # Extract major version (e.g., "aws-cli/2.13.5" → 2)
  local major_version
  major_version=$(echo "$version_output" | sed -n 's/^aws-cli\/\([0-9]\).*/\1/p')
  
  if [[ -z "$major_version" ]] || [[ "$major_version" -lt 2 ]]; then
    cat >&2 << 'UPGRADE_MSG'
⚠️  WARNING: AWS CLI v1 detected or version check failed.

AWS CLI v1 does not return the 'Throughput' field for EBS volumes, which is needed
for accurate GP3 analysis. The script will still work but ThroughputMBps may be null.

RECOMMENDED FIX:
1. Remove the old yum-installed CLI:
   yum remove awscli -y

2. Download and install AWS CLI v2:
   cd /tmp
   curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
   yum install -y unzip
   unzip awscliv2.zip
   ./aws/install

3. Verify:
   aws --version
   which aws  # Should be /usr/local/bin/aws

4. Re-run this script to get Throughput data.

UPGRADE_MSG
  fi
}

check_aws_cli_version

# ── Instance metadata ────────────────────────────────────────────────────────
INSTANCE_ID=""
REGION=""
INSTANCE_TYPE=""
AZ=""
REMOTE_MODE="false"

if [[ -n "$INSTANCE_ID_ARG" ]]; then
  REMOTE_MODE="true"
  INSTANCE_ID="$INSTANCE_ID_ARG"
  REGION="$REGION_ARG"

  DESCRIBE_ARGS=()
  if [[ -n "$REGION" ]]; then
    DESCRIBE_ARGS+=(--region "$REGION")
  fi

  INSTANCE_INFO=$(aws ec2 describe-instances \
    "${DESCRIBE_ARGS[@]}" \
    --instance-ids "$INSTANCE_ID" \
    --query 'Reservations[0].Instances[0].{InstanceType: InstanceType, AvailabilityZone: Placement.AvailabilityZone}' \
    --output json)

  INSTANCE_TYPE=$(jq -r '.InstanceType // empty' <<< "$INSTANCE_INFO")
  AZ=$(jq -r '.AvailabilityZone // empty' <<< "$INSTANCE_INFO")

  if [[ -z "$INSTANCE_TYPE" ]]; then
    die "instance not found or not accessible: $INSTANCE_ID"
  fi

  # If region was not provided, infer it from availability zone (e.g. us-east-1a -> us-east-1).
  if [[ -z "$REGION" && -n "$AZ" ]]; then
    REGION="${AZ::-1}"
  fi
else
  TOKEN=$(curl -s -f -X PUT "http://169.254.169.254/latest/api/token" \
    -H "X-aws-ec2-metadata-token-ttl-seconds: 21600") || {
    echo '{"error": "Failed to obtain IMDSv2 token. Is this an EC2 instance? If running from CloudShell, pass --instance-id."}' >&2
    exit 1
  }

  imds() {
    curl -s -f -H "X-aws-ec2-metadata-token: $TOKEN" \
      "http://169.254.169.254/latest/meta-data/$1"
  }

  INSTANCE_ID=$(imds instance-id)
  REGION=$(imds placement/region)
  INSTANCE_TYPE=$(imds instance-type)
  AZ=$(imds placement/availability-zone)
fi

# ── Volume details via AWS CLI ───────────────────────────────────────────────
# Query volumes and post-process to add baseline throughput info for fixed-throughput types
AWS_REGION_ARGS=()
if [[ -n "$REGION" ]]; then
  AWS_REGION_ARGS+=(--region "$REGION")
fi

VOLUMES=$(aws ec2 describe-volumes \
  "${AWS_REGION_ARGS[@]}" \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" \
  --query 'Volumes[*]' \
  --output json | jq '
  map(
    .VolumeId as $vid |
    .Attachments[0].Device as $dev |
    .VolumeType as $type |
    .Size as $size |
    .Iops as $iops |
    .Throughput as $tp |
    .Encrypted as $enc |
    .KmsKeyId as $kms |
    .State as $state |
    .MultiAttachEnabled as $multi |
    .SnapshotId as $snap |
    {
      VolumeId: $vid,
      Device: $dev,
      VolumeType: $type,
      SizeGiB: $size,
      IOPS: $iops,
      ThroughputMBps: (
        if $type == "gp3" or $type == "io2" then $tp
        elif $type == "gp2" then null
        elif $type == "io1" then null
        elif $type == "st1" then null
        elif $type == "sc1" then null
        else $tp
        end
      ),
      ThroughputNote: (
        if $type == "gp2" then "gp2: 125 MB/s baseline (varies by size)"
        elif $type == "io1" then "io1: 250 MB/s baseline"
        elif $type == "st1" then "st1: 500 MB/s burst / 125 MB/s baseline"
        elif $type == "sc1" then "sc1: 250 MB/s burst / 80 MB/s baseline"
        elif $type == "io2" then "io2: configurable (default 1000 MB/s)"
        elif $type == "gp3" then "gp3: configurable"
        else null
        end
      ),
      Encrypted: $enc,
      KmsKeyId: $kms,
      State: $state,
      MultiAttach: $multi,
      SnapshotId: $snap
    }
  )
')

# ── Block device / filesystem / mount info ───────────────────────────────────
if [[ "$REMOTE_MODE" == "true" ]]; then
  # In CloudShell/local mode we cannot inspect remote OS block devices or mounts.
  LSBLK_JSON='{"blockdevices":[]}'
  MOUNTS_JSON='[]'
  VOL_MAP='{}'
else
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
  # Flatten block devices one level (disk + partitions) keyed by device name
  [ $lsblk.blockdevices[] | ., (.children // [])[] ] as $all_blk |
  ($all_blk | map({key: .name, value: .}) | from_entries) as $blk_by_dev |

  # Mount lookup keyed by device name
  ($mounts | map({key: .device, value: .}) | from_entries) as $mount_by_dev |

  # Enrich each EBS volume with its OS device name and mount info
  [
    $volumes[] |
    . as $vol |
    ($vol_map[$vol.VolumeId]) as $os_dev |
    # Prefer a direct mount; fall back to first mounted partition
    (
      if $os_dev == null then
        null
      elif $mount_by_dev[$os_dev] != null then
        $mount_by_dev[$os_dev]
      else
        [ (($blk_by_dev[$os_dev] // {}).children // [])[] |
          $mount_by_dev[.name] | select(. != null) ] | .[0]
      end
    ) as $mnt |
    # For gp3 volumes, set default throughput if missing (125 MB/s is the minimum)
    (
      if ($vol.VolumeType == "gp3" and ($vol.ThroughputMBps == null or $vol.ThroughputMBps == 0)) then
        125
      else
        $vol.ThroughputMBps
      end
    ) as $throughput |
    $vol + {
      ThroughputMBps: $throughput,
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
