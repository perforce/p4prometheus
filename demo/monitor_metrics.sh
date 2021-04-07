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
data_file=/p4/monitor_metrics.dat
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
 
monitor_metrics.sh [<instance> | -nosdp] [-p <port>] | [-u <user>] | [-m <metrics_dir>] [-d <data_file>]
 
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
        (-d) data_file=$2; shiftArgs=1;;
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
    check_for_replica=$($p4 info | grep -c 'Replica of:')
    if [[ "$check_for_replica" -eq "0" ]]; then
        P4REPLICA="FALSE"
    else
        P4REPLICA="TRUE"
    fi
fi

# Get server id
SERVER_ID=$($p4 serverid | awk '{print $3}')
SERVER_ID=${SERVER_ID:-noserverid}
serverid_label="serverid=\"$SERVER_ID\""

# Set data vars
tmpdatafile="${data_file}.tmp"
p4log_ts_last=0
p4log_lc_last=0
p4err_ts_last=0
p4err_lc_last=0 

load_data_file () {
    # Loads the data from the data file if it exists, otherwise sets all vars to 0
    [[ -f "$data_file" ]] || { echo "data_file not found"; return ; } 
    
    # Read in the data file information
    while read filetype mod_time line_count
    do
        case "$filetype" in
        'p4log')
            p4log_ts_last=$mod_time
            p4log_lc_last=$line_count
        ;;
        'p4err')
            p4err_ts_last=$mod_time
            p4err_lc_last=$line_count
        ;;
        esac
    done < $data_file
    echo "data file loaded:"
    echo "[p4log][last timestamp: ${p4log_ts_last}][last linecount: ${p4log_lc_last}]"
    echo "[p4err][last timestamp: ${p4err_ts_last}][last linecount: ${p4err_lc_last}]"
}

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
    chmod 644 "$tmpfname"
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
        chmod 644 "$tmpfname"
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
    echo "p4_process_count{${serverid_label}${sdpinst_label}} $pcount" >> "$tmpfname"

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_completed_cmds () {
    # Metric for completed commands by parsing log file - auto-skipped for large log files
    local num_cmds=0
    fname="$metrics_root/p4_completed_cmds${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"

    # If the logfile doesnt exist delete prom and return
    [[ -f "$p4logfile" ]] || { rm -f "$fname"; return ; }

    # This test is skipped if the log file is bigger than 1GB for performance reasons
    fsize=$(du -k "$p4logfile" | cut -f 1)
    if [[ "$fsize" -gt 1000000 ]]; then
        return
    fi

    # Get the current timestamp and linecount
    p4log_ts_curr=$(stat -c %Y $p4logfile)
    p4log_lc_curr=$(wc -l $p4logfile | awk '{print $1}')
    # Update the data file
    echo "Updating data file:"
    echo "[p4log][curr timestamp: ${p4log_ts_curr}][curr linecount: ${p4log_lc_curr}]"
    echo "p4log $p4log_ts_curr $p4log_lc_curr" >> $tmpdatafile

    # If the logfile current timestamp is less then the last timestamp delete prom and return
    [[ $p4log_ts_curr -gt $p4log_ts_last ]] || { rm -f "$fname"; return ; }

    # If the linecount current is greater then the last, then set the lines to read in
    if [[ $p4log_lc_curr -gt $p4log_lc_last ]]; then
        num_cmds=$(sed -n "$p4log_lc_last,$p4log_lc_curr"p "$p4logfile" | grep -c ' completed ')
    else
        num_cmds=$(grep -c ' completed ' "$p4logfile")
    fi
    echo "#HELP p4_completed_cmds Completed p4 commands" > "$tmpfname"
    echo "#TYPE p4_completed_cmds counter" >> "$tmpfname"
    echo "p4_completed_cmds{${serverid_label}${sdpinst_label}} $num_cmds" >> "$tmpfname"
    chmod 644 "$tmpfname"
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
#    for f in $(ls -t /p4/$SDP_INSTANCE/logs/checkpoint.log*);
    for f in $(find -L /p4/$SDP_INSTANCE/logs -type f -name checkpoint.log* -exec ls -t {} +)
    do
        if [[ `grep -cE "Start p4_$SDP_INSTANCE Checkpoint|End p4_$SDP_INSTANCE Checkpoint" $f` -eq 2 ]]; then
            ckp_log="$f"
            break
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

    chmod 644 "$tmpfname"
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

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_errors () {
    # Metric for error counts - but only if structured error log exists
    fname="$metrics_root/p4_errors${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    
    [[ -f "$errors_file" ]] || { rm -f "$fname"; return; }

    declare -A subsystems=([0]=OS [1]=SUPP [2]=LBR [3]=RPC [4]=DB [5]=DBSUPP [6]=DM [7]=SERVER [8]=CLIENT \
    [9]=INFO [10]=HELP [11]=SPEC [12]=FTPD [13]=BROKER [14]=P4QT [15]=X3SERVER [16]=GRAPH [17]=SCRIPT \
    [18]=SERVER2 [19]=DM2)

    # Log format differs according to p4d versions - first column
    ver=$(head -1 "$errors_file" | awk -F, '{print $1}')
    if [[ "$ver" == "4" ]]; then
        indID=15
    elif [[ "$ver" == "4.50" ]]; then
        indID=17
    fi
    indSS=$((indID+1))
    indError=$((indID-1))

    echo "" > "$tmpfname"
    echo "#HELP p4_error_count Server errors by id" >> "$tmpfname"
    echo "#TYPE p4_error_count counter" >> "$tmpfname"
    while read count ss_id error_id level
    do
        subsystem=${subsystems[$ss_id]}
        [[ -z "$subsystem" ]] && subsystem=$ss_id
        echo "p4_error_count{${serverid_label}${sdpinst_label},subsystem=\"$subsystem\",error_id=\"$error_id\",level=\"$level\"} $count" >> "$tmpfname"
    done < <(awk -F, -v indID="$indID" -v indSS="$indSS" -v indError="$indError" '{printf "%s %s %s\n", $indID, $indSS, $indError}' "$errors_file" | sort | uniq -c)

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_pull () {
    # p4 pull metrics - only valid for replica servers
    [[ "${P4REPLICA}" == "TRUE" ]] || return

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

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_realtime () {
    # p4d --show-realtime - only for 2021.1 or greater
    # Intially only available for SDP
    [[ $UseSDP -eq 1 ]] || return
    p4dver=$($P4DBIN -V |grep Rev.|awk -F / '{print $3}' )
    [[ "$p4dver" > "2020.0" ]] || return

    realtimefile="/tmp/show-realtime.out"
    $P4DBIN --show-realtime > "$realtimefile" 2> /dev/null || return

    # File format:
    # rtv.db.lockwait (flags 0) 0 max 382
    # rtv.db.ckp.active (flags 0) 0
    # rtv.db.ckp.records (flags 0) 34 max 34
    # rtv.db.io.records (flags 0) 126389592854
    # rtv.rpl.behind.bytes (flags 0) 0 max -1
    # rtv.rpl.behind.journals (flags 0) 0 max -1
    # rtv.svr.sessions.active (flags 0) 110 max 585
    # rtv.svr.sessions.total (flags 0) 5997080

    fname="$metrics_root/p4_realtime${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"

    echo "" > "$tmpfname"

    origname="rtv.db.lockwait"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime lockwait counter" >> "$tmpfname"
    echo "# TYPE $mname gauge" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    origname="rtv.db.ckp.active"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime checkpoint active indicator" >> "$tmpfname"
    echo "# TYPE $mname gauge" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    origname="rtv.db.ckp.records"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime checkpoint records counter" >> "$tmpfname"
    echo "# TYPE $mname gauge" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    origname="rtv.db.io.records"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime IO records counter" >> "$tmpfname"
    echo "# TYPE $mname counter" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    origname="rtv.rpl.behind.bytes"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime replica bytes lag counter" >> "$tmpfname"
    echo "# TYPE $mname gauge" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    origname="rtv.rpl.behind.journals"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime replica journal lag counter" >> "$tmpfname"
    echo "# TYPE $mname gauge" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    origname="rtv.svr.sessions.active"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime server active sessions counter" >> "$tmpfname"
    echo "# TYPE $mname gauge" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    origname="rtv.svr.sessions.total"
    mname="p4_${origname//./_}"
    echo "# HELP $mname P4 realtime server total sessions counter" >> "$tmpfname"
    echo "# TYPE $mname counter" >> "$tmpfname"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

update_data_file () {
    echo "Updating data file:"
    cat $tmpdatafile
    rm -f $data_file
    mv $tmpdatafile $data_file
}

load_data_file
monitor_uptime
monitor_change
monitor_processes
monitor_completed_cmds
monitor_checkpoint
monitor_replicas
monitor_errors
monitor_pull
monitor_realtime
update_data_file

# Make sure all readable by node_exporter or other user
chmod 644 $metrics_root/*.prom
