#!/bin/bash
# Generate Perforce Helix Core Server monitoring metrics for use with Prometheus (collected via node_explorer)
# Put this job into perforce user (whatever user perforce processes are run as) crontab:
#
# If using SDP:
#   */1 * * * * /p4/common/site/bin/monitor_metrics.sh $INSTANCE > /dev/null 2>&1 ||:
# Otherwise:
#   */1 * * * * /path/to/monitor_metrics.sh -p $P4PORT -u $P4USER -nosdp > /dev/null 2>&1 ||:
# If not using SDP then please ensure that appropriate LONG TERM TICKET is setup in the environment
# that this script is running.
#
# Please note you need to make sure that the specified directory below (which may be linked)
# can be read by the node_exporter user (and is setup via --collector.textfile.directory parameter)
#
# Note we use a tempfile for each metric to avoid partial reads. Textfile collector only looks for files
# ending in .prom so we do a final rename when ready

if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# ============================================================
# Configuration section

# This might also be /hxlogs/metrics or passed as a parameter (with -m flag)
metrics_root=/p4/metrics
# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage
{
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for monitor_metrics.sh:
 
monitor_metrics.sh [<instance> | -nosdp] [-p <port>] | [-u <user>] | [-m <metrics_dir>]
 
   or
 
monitor_metrics.sh -h
"
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i UseSDP=1

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h;;
        # (-man) usage -man;;
        (-p) Port=$2; shiftArgs=1;;
        (-u) User=$2; shiftArgs=1;;
        (-m) metrics_root=$2; shiftArgs=1;; 
        (-nosdp) UseSDP=0;;
        (-*) usage -h "Unknown command line option ($1)." && exit 1;;
        (*) export SDP_INSTANCE=$1;;
    esac
 
    # Shift (modify $#) the appropriate number of times.
    shift; while [[ "$shiftArgs" -gt 0 ]]; do
        [[ $# -eq 0 ]] && usage -h "Incorrect number of arguments."
        shiftArgs=$shiftArgs-1
        shift
    done
done
set -u

[[ -d "$metrics_root" ]] || bail "Specified metrics directory '$metrics_root' does not exist!"

if [[ $UseSDP -eq 1 ]]; then
    SDP_INSTANCE=${SDP_INSTANCE:-Unset}
    SDP_INSTANCE=${1:-$SDP_INSTANCE}
    if [[ $SDP_INSTANCE == Unset ]]; then
        echo -e "\\nError: Instance parameter not supplied.\\n"
        echo "You must supply the Perforce SDP instance as a parameter to this script."
        exit 1
    fi

    # Load SDP controlled shell environment.
    # shellcheck disable=SC1091
    source /p4/common/bin/p4_vars "$SDP_INSTANCE" ||\
    { echo -e "\\nError: Failed to load SDP environment.\\n"; exit 1; }

    p4="$P4BIN -u $P4USER -p $P4PORT"
    $p4 info -s || bail "Can't connect to P4PORT: $P4PORT"
    sdpinst_label=",sdpinst=\"$SDP_INSTANCE\""
    sdpinst_suffix="-$SDP_INSTANCE"
    p4logfile="$P4LOG"
    errors_file="$LOGS/errors.csv"
else
    p4port=${Port:-$P4PORT}
    p4user=${User:-$P4USER}
    p4="p4 -u $p4user -p $p4port"
    $p4 info -s || bail "Can't connect to P4PORT: $p4port"
    sdpinst_label=""
    sdpinst_suffix=""
    p4logfile=$($p4 configure show | grep P4LOG | sed -e 's/P4LOG=//' -e 's/ .*//')
    errors_file=$($p4 configure show | egrep "serverlog.file.*errors.csv" | cut -d= -f2 | sed -e 's/ (.*//')

fi

# Get server id
SERVER_ID=$($p4 serverid | awk '{print $3}')
SERVER_ID=${SERVER_ID:-noserverid}
serverid_label="serverid=\"$SERVER_ID\""

monitor_uptime () {
    # Server uptime as a simple seconds parameter - parsed from p4 info:
    # Server uptime: 168:39:20
    fname="$metrics_root/p4_uptime${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    uptime=$($p4 info 2>&1 | grep uptime | awk '{print $3}')
    [[ -z "$uptime" ]] && uptime="0:0:0"
    uptime=${uptime//:/ }
    arr=($uptime)
    hours=${arr[0]}
    mins=${arr[1]}
    secs=${arr[2]}
    #echo $hours $mins $secs
    # Ensure base 10 arithmetic used to avoid overflow errors
    uptime_secs=$(((10#$hours * 3600) + (10#$mins * 60) + 10#$secs))
    echo "# HELP p4_server_uptime P4D Server uptime (seconds)" > "$tmpfname"
    echo "# TYPE p4_server_uptime counter" >> "$tmpfname"
    echo "p4_server_uptime{${serverid_label}${sdpinst_label}} $uptime_secs" >> "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_change () {
    # Latest changelist counter as single counter value
    fname="$metrics_root/p4_change${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    curr_change=$($p4 counters 2>&1 | grep change | awk '{print $3}')
    if [[ ! -z "$curr_change" ]]; then
        echo "# HELP p4_change_counter P4D change counter" > "$tmpfname"
        echo "# TYPE p4_change_counter counter" >> "$tmpfname"
        echo "p4_change_counter{${serverid_label}${sdpinst_label}} $curr_change" >> "$tmpfname"
        mv "$tmpfname" "$fname"
    fi
}

monitor_processes () {
    # Monitor metrics summarised by cmd or user
    fname="$metrics_root/p4_monitor${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    monfile="/tmp/mon.out"

    $p4 monitor show > "$monfile" 2> /dev/null
    echo "# HELP p4_monitor_by_cmd P4 running processes" > "$tmpfname"
    echo "# TYPE p4_monitor_by_cmd counter" >> "$tmpfname"
    awk '{print $5}' "$monfile" | sort | uniq -c | while read count cmd
    do
        echo "p4_monitor_by_cmd{${serverid_label}${sdpinst_label},cmd=\"$cmd\"} $count" >> "$tmpfname"
    done

    echo "# HELP p4_monitor_by_user P4 running processes" >> "$tmpfname"
    echo "# TYPE p4_monitor_by_user counter" >> "$tmpfname"
    awk '{print $3}' "$monfile" | sort | uniq -c | while read count user
    do
        echo "p4_monitor_by_user{${serverid_label}${sdpinst_label},user=\"$user\"} $count" >> "$tmpfname"
    done

    if [[ $UseSDP -eq 1 ]]; then
        proc="p4d_${SDP_INSTANCE}"
    else
        proc="p4d"
    fi
    echo "# HELP p4_process_count P4 ps running processes" >> "$tmpfname"
    echo "# TYPE p4_process_count counter" >> "$tmpfname"
    pcount=$(ps ax | grep "$proc " | grep -v "grep $proc" | wc -l)
    echo "p4_process_count{serverid=\"$SERVER_ID\",sdpinst=\"$SDP_INSTANCE\"} $pcount" >> "$tmpfname"

    mv "$tmpfname" "$fname"
}

monitor_completed_cmds () {
    # Metric for completed commands by parsing log file - might be considered expensive to compute as log grows.
    fname="$metrics_root/p4_completed_cmds${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    [[ -f "$p4logfile" ]] || return
    num_cmds=$(grep -c ' completed ' "$p4logfile")
    echo "#HELP p4_completed_cmds_per_day Completed p4 commands" > "$tmpfname"
    echo "#TYPE p4_completed_cmds_per_day counter" >> "$tmpfname"
    echo "p4_completed_cmds_per_day{${serverid_label}${sdpinst_label}} $num_cmds" >> "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_checkpoint () {
    # Metric for when SDP checkpoint last ran and how long it took.
    # Not as easy as it might first appear because:
    # - might be in progress
    # - multiple rotate_journal.sh may be run in between daily_checkpoint.sh - and they
    # both write to checkpoint.log!
    # The strings searched for have been present in SDP logs for years now...

    [[ $UseSDP -eq 0 ]] && return   # Not valid if SDP not in use
    
    fname="$metrics_root/p4_checkpoint${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"

    echo "#HELP p4_sdp_checkpoint_log_time Time of last checkpoint log" > "$tmpfname"
    echo "#TYPE p4_sdp_checkpoint_log_time gauge" >> "$tmpfname"

    # Look for latest checkpoint log which has Start/End (avoids run in progress and rotate_journal logs)
    ckp_log=""
    for f in $(ls -tr /p4/$SDP_INSTANCE/logs/checkpoint.log*);
    do
        if [[ `grep -cE "Start p4_$SDP_INSTANCE Checkpoint|End p4_$SDP_INSTANCE Checkpoint" $f` -eq 2 ]]; then
            ckp_log="$f"
        fi;
    done
    ckp_time=0
    if [[ ! -z "$ckp_log" ]]; then
        ckp_time=$(date -r "$ckp_log" +"%s")
    fi
    echo "p4_sdp_checkpoint_log_time{${serverid_label}${sdpinst_label}} $ckp_time" >> "$tmpfname"

    echo "#HELP p4_sdp_checkpoint_duration Time taken for last checkpoint/restore action" >> "$tmpfname"
    echo "#TYPE p4_sdp_checkpoint_duration gauge" >> "$tmpfname"

    ckp_duration=0
    if [[ ! -z "$ckp_log" && $ckp_time -gt 0 ]]; then
        dt=$(grep "Start p4_$SDP_INSTANCE Checkpoint" "$ckp_log" | sed -e 's/\/p4.*//')
        start_time=$(date -d "$dt" +"%s")
        dt=$(grep "End p4_$SDP_INSTANCE Checkpoint" "$ckp_log" | sed -e 's/\/p4.*//')
        end_time=$(date -d "$dt" +"%s")
        ckp_duration=$(($end_time - $start_time))
    fi
    echo "p4_sdp_checkpoint_duration{${serverid_label}${sdpinst_label}} $ckp_duration" >> "$tmpfname"

    mv "$tmpfname" "$fname"
}

monitor_replicas () {
    # Metric for server replicas
    fname="$metrics_root/p4_replication${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"

    valid_servers=""
    # Read like this to set global variables in loop
    while read svr_id type services
    do
        if [[ $services =~ standard|replica|commit-server|edge-server|forwarding-replica|build-server|standby|forwarding-standby ]]; then
            valid_servers="$valid_servers $svr_id"
        fi
    done < <($p4 -F "%serverID% %type% %services%" servers)
    declare -A svr_jnl
    declare -A svr_pos
    while read svr_id jnl pos
    do
        svr_jnl[$svr_id]=$jnl
        svr_pos[$svr_id]=$pos
    done < <($p4 -F "%serverID% %appliedJnl% %appliedPos%" servers -J)

    echo "#HELP p4_replica_curr_jnl Current journal for server" >> "$tmpfname"
    echo "#TYPE p4_replica_curr_jnl counter" >> "$tmpfname"
    for s in $valid_servers
    do
        curr_jnl=${svr_jnl[$s]:-0}
        curr_jnl=${curr_jnl:-0}
        echo "p4_replica_curr_jnl{${serverid_label}${sdpinst_label},servername=\"$s\"} $curr_jnl" >> "$tmpfname"
    done

    echo "#HELP p4_replica_curr_pos Current journal for server" >> "$tmpfname"
    echo "#TYPE p4_replica_curr_pos counter" >> "$tmpfname"
    for s in $valid_servers
    do
        curr_pos=${svr_pos[$s]:-0}
        curr_pos=${curr_pos:-0}
        echo "p4_replica_curr_pos{${serverid_label}${sdpinst_label},servername=\"$s\"} $curr_pos" >> "$tmpfname"
    done

    mv "$tmpfname" "$fname"
}

monitor_errors () {
    # Metric for error counts - but only if structured error log exists
    [[ -f "$errors_file" ]] || return
    fname="$metrics_root/p4_errors${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    echo "" > "$tmpfname"

    declare -A subsystems=([0]=OS [1]=SUPP [2]=LBR [3]=RPC [4]=DB [5]=DBSUPP [6]=DM [7]=SERVER [8]=CLIENT \
    [9]=INFO [10]=HELP [11]=SPEC [12]=FTPD [13]=BROKER [14]=P4QT [15]=X3SERVER [16]=GRAPH [17]=SCRIPT \
    [18]=SERVER2 [19]=DM2)

    echo "#HELP p4_error_count Server errors by id" >> "$tmpfname"
    echo "#TYPE p4_error_count counter" >> "$tmpfname"
    while read count ss_id error_id level
    do
        subsystem=${subsystems[$ss_id]}
        [[ -z "$subsystem" ]] && subsystem=$ss_id
        echo "p4_error_count{${serverid_label}${sdpinst_label},subsystem=\"$subsystem\",error_id=\"$error_id\",level=\"$level\"} $count" >> "$tmpfname"
    done < <(awk -F, '{printf "%s %s %s\n", $15,$16,$14}' "$errors_file" | sort | uniq -c)

    mv "$tmpfname" "$fname"
}

monitor_pull () {
    # p4 pull metrics - only valid for replica servers
    $p4 pull -lj || return

    fname="$metrics_root/p4_pull${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    pullfile="/tmp/pull.out"
    $p4 pull -l > "$pullfile" 2> /dev/null 
    echo "# HELP p4_pull_errors P4 pull transfers failed count" > "$tmpfname"
    echo "# TYPE p4_pull_errors counter" >> "$tmpfname"
    count=$(grep -cEa "failed\.$" "$pullfile")
    echo "p4_pull_errors{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
 
    echo "# HELP p4_pull_queue P4 pull files in queue count" >> "$tmpfname"
    echo "# TYPE p4_pull_queue counter" >> "$tmpfname"
    count=$(grep -cvEa "failed\.$" "$pullfile")
    echo "p4_pull_queue{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
 
    mv "$tmpfname" "$fname"
}


monitor_uptime
monitor_change
monitor_processes
monitor_completed_cmds
monitor_checkpoint
monitor_replicas
monitor_errors
monitor_pull

# Make sure all readable by node_exporter or other user
chmod 755 $metrics_root/*.prom
