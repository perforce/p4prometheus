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

: See https://johannes.truschnigg.info/writing/2021-12_colodebug/
if [[ -n ${COLODEBUG} && ${-} != *x* ]]; then
:() {
    [[ ${1:--} != ::* ]] && return 0
    printf '%s\n' "${*}" >&2
}
fi

# ============================================================
# Configuration section

# In an SDP env, this might also be /hxlogs/metrics - see install_p4prom.sh
# In a non-SDP env, this directory might be passed as a parameter (with -m flag) or configure here as, e.g.
#   /p4metrics or /var/p4metrics
# It is IMPORTANT that your config file for the node_exporter service is correctly configured
# with parameter specifying the same directory as here:  
#   --collector.textfile.directory=/p4metrics
# The directory permissions are also important. Whatever directory you specify must:
#   - have read access for OS user running node_exporter service (e.g. run chmod 755 on the dir and any parent dir)
#   - have write access for OS user running this monitor_metrics.sh (e.g. chown <user> )
# So in a non-SDP env, it might be simplest to create a single top level dir. /p4metrics or similar 

metrics_root=/p4/metrics

# For non SDP, if the 'p4' executable is not in your $PATH, then specify full path here. SDP will update it.

P4BIN=p4

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
 
monitor_metrics.sh [<instance> | -nosdp [-p <port>] | [-u <user>] ] | [-m <metrics_dir>]
 
   or
 
monitor_metrics.sh -h
"
}

: Command Line Processing
 
declare -i shiftArgs=0
declare -i UseSDP=1

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 0;;
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

: Following file is used to cache "p4 info"
tmp_info_data="$metrics_root/tmp_info.dat"

if [[ $UseSDP -eq 1 ]]; then
    SDP_INSTANCE=${SDP_INSTANCE:-Unset}
    SDP_INSTANCE=${1:-$SDP_INSTANCE}
    if [[ $SDP_INSTANCE == Unset ]]; then
        echo -e "\\nError: Instance parameter not supplied.\\n"
        echo "You must supply the Perforce SDP instance as a parameter to this script."
        exit 1
    fi

    # Since metrics root is shared, adjust tmp_info_data when using SDP:
    tmp_info_data="$metrics_root/tmp_info-${SDP_INSTANCE}.dat"

    # Load SDP controlled shell environment.
    # shellcheck disable=SC1091
    source /p4/common/bin/p4_vars "$SDP_INSTANCE" ||\
    { echo -e "\\nError: Failed to load SDP environment.\\n"; exit 1; }

    # Note that P4BIN is defined by SDP by sourcing above file, as are P4USER, P4PORT
    p4="$P4BIN -u $P4USER -p $P4PORT"
    $p4 info -s > "$tmp_info_data"
    [[ $? -eq 0 ]] || bail "Can't connect to P4PORT: $P4PORT"
    sdpinst_label=",sdpinst=\"$SDP_INSTANCE\""
    sdpinst_suffix="-$SDP_INSTANCE"
    p4logfile="$P4LOG"
    errors_file="$LOGS/errors.csv"
else
    p4port=${Port:-$P4PORT}
    p4user=${User:-$P4USER}
    p4="$P4BIN -u $p4user -p $p4port"
    $p4 info -s > "$tmp_info_data"
    [[ $? -eq 0 ]] || bail "Can't connect to P4PORT: $p4port"
    sdpinst_label=""
    sdpinst_suffix=""
    tmp_config_data="$metrics_root/tmp_config_show.dat"
    $p4 configure show > "$tmp_config_data"
    p4logfile=$(grep P4LOG "$tmp_config_data" | sed -e 's/P4LOG=//' -e 's/ .*//')
    errors_file=$(egrep "serverlog.file.*errors.csv" "$tmp_config_data" | cut -d= -f2 | sed -e 's/ (.*//')
    check_for_replica=$(grep -c 'Replica of:' "$tmp_info_data")
    if [[ "$check_for_replica" -eq "0" ]]; then
        P4REPLICA="FALSE"
    else
        P4REPLICA="TRUE"
    fi
fi

# Get server id. Usually server.id files are a single line containing the
# ServerID value. However, a server.id file will have a second line if a
# 'p4 failover' was done containing an error message displayed to users
# during the failover, and also preventing the service from starting
# post-failover (to avoid split brain). For purposes of this check, we care
# only about the ServerID value contained on the first line, so we use
# 'head -1' on the server.id file.
if [[ -r "${P4ROOT:-UnsetP4ROOT}/server.id" ]]; then
   SERVER_ID=$(head -1 "$P4ROOT/server.id" 2>/dev/null)
else
   SERVER_ID=$($p4 serverid 2>/dev/null | awk '{print $3}')
fi
[[ -n "$SERVER_ID" ]] || SERVER_ID=UnsetServerID
serverid_label="serverid=\"$SERVER_ID\""

monitor_uptime () {
    # Server uptime as a simple seconds parameter - parsed from p4 info:
    # Server uptime: 168:39:20
    fname="$metrics_root/p4_uptime${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    uptime=$(grep uptime "$tmp_info_data" | awk '{print $3}')
    [[ -z "$uptime" ]] && uptime="0:0:0"
    uptime=${uptime//:/ }
    arr=($uptime)
    hours=${arr[0]}
    mins=${arr[1]}
    secs=${arr[2]}
    #echo $hours $mins $secs
    # Ensure base 10 arithmetic used to avoid overflow errors
    uptime_secs=$(((10#$hours * 3600) + (10#$mins * 60) + 10#$secs))
    rm -f "$tmpfname"
    echo "# HELP p4_server_uptime P4D Server uptime (seconds)" >> "$tmpfname"
    echo "# TYPE p4_server_uptime counter" >> "$tmpfname"
    echo "p4_server_uptime{${serverid_label}${sdpinst_label}} $uptime_secs" >> "$tmpfname"
    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_license () {
    # Server license expiry - parsed from "p4 license -u" - key fields:
    # ... userCount 893
    # ... userLimit 1000
    # ... licenseExpires 1677628800
    # ... licenseTimeRemaining 34431485
    # ... supportExpires 1677628800
    # Note that sometimes you only get supportExpires - we calculate licenseTimeRemaining in that case
    fname="$metrics_root/p4_license${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    if [[ $UseSDP -eq 1 ]]; then
        tmp_license_data="$metrics_root/tmp_license-${SDP_INSTANCE}"
    else
        tmp_license_data="$metrics_root/tmp_license"
    fi
    # Don't update if there is no license for this server, e.g. a replica
    no_license=$(grep -c "Server license: none" "$tmp_info_data")
    # Update every 60 mins
    [[ ! -f "$tmp_license_data" || $(find "$tmp_license_data" -mmin +60) ]] || return
    $p4 license -u 2>&1 > "$tmp_license_data"
    [[ $? -ne 0 ]] && return

    userCount=0
    userLimit=0
    licenseExpires=0
    licenseTimeRemaining=0
    supportExpires=0
    licenseInfo=""
    licenseInfo_label=""
    licenseIP=""
    licenseIP_label=""

    if [[ $no_license -ne 1 ]]; then
        userCount=$(grep userCount $tmp_license_data | awk '{print $3}')
        userLimit=$(grep userLimit $tmp_license_data | awk '{print $3}')
        licenseExpires=$(grep "\.\.\. licenseExpires" $tmp_license_data | awk '{print $3}')
        licenseTimeRemaining=$(grep "\.\.\. licenseTimeRemaining" $tmp_license_data | awk '{print $3}')
        supportExpires=$(grep "\.\.\. supportExpires" $tmp_license_data | awk '{print $3}')
        licenseInfo=$(grep "Server license: " "$tmp_info_data" | sed -e "s/Server license: //" | sed -Ee "s/\(expires [^\)]+\)//" | sed -Ee "s/\(support [^\)]+\)//" )
        if [[ -z $licenseTimeRemaining && ! -z $supportExpires ]]; then
            dt=$(date +%s)
            licenseTimeRemaining=$(($supportExpires - $dt))
        fi
        # Trim trailing spaces
        licenseInfo=$(echo $licenseInfo | sed -Ee 's/[ ]+$//')
        licenseIP=$(grep "Server license-ip: " "$tmp_info_data" | sed -e "s/Server license-ip: //")
    fi

    licenseInfo_label=",info=\"${licenseInfo:-none}\""
    licenseIP_label=",IP=\"${licenseIP:-none}\""

    rm -f "$tmpfname"
    echo "# HELP p4_licensed_user_count P4D Licensed User count" >> "$tmpfname"
    echo "# TYPE p4_licensed_user_count gauge" >> "$tmpfname"
    echo "p4_licensed_user_count{${serverid_label}${sdpinst_label}} $userCount" >> "$tmpfname"
    echo "# HELP p4_licensed_user_limit P4D Licensed User Limit" >> "$tmpfname"
    echo "# TYPE p4_licensed_user_limit gauge" >> "$tmpfname"
    echo "p4_licensed_user_limit{${serverid_label}${sdpinst_label}} $userLimit" >> "$tmpfname"
    if [[ ! -z $licenseExpires ]]; then
        echo "# HELP p4_license_expires P4D License expiry (epoch secs)" >> "$tmpfname"
        echo "# TYPE p4_license_expires gauge" >> "$tmpfname"
        echo "p4_license_expires{${serverid_label}${sdpinst_label}} $licenseExpires" >> "$tmpfname"
    fi
    echo "# HELP p4_license_time_remaining P4D License time remaining (secs)" >> "$tmpfname"
    echo "# TYPE p4_license_time_remaining gauge" >> "$tmpfname"
    echo "p4_license_time_remaining{${serverid_label}${sdpinst_label}} $licenseTimeRemaining" >> "$tmpfname"
    if [[ ! -z $supportExpires ]]; then
        echo "# HELP p4_license_support_expires P4D License support expiry (epoch secs)" >> "$tmpfname"
        echo "# TYPE p4_license_support_expires gauge" >> "$tmpfname"
        echo "p4_license_support_expires{${serverid_label}${sdpinst_label}} $supportExpires" >> "$tmpfname"
    fi
    echo "# HELP p4_license_info P4D License info" >> "$tmpfname"
    echo "# TYPE p4_license_info gauge" >> "$tmpfname"
    echo "p4_license_info{${serverid_label}${sdpinst_label}${licenseInfo_label}} 1" >> "$tmpfname"
    echo "# HELP p4_license_IP P4D Licensed IP" >> "$tmpfname"
    echo "# TYPE p4_license_IP" >> "$tmpfname"
    echo "p4_license_IP{${serverid_label}${sdpinst_label}${licenseIP_label}} 1" >> "$tmpfname"

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_filesys () {
    # Log current filesys.*.min settings
    # p4 configure show can give 2 values, or just the (default)
    #   filesys.P4ROOT.min=5G (configure)
    #   filesys.P4ROOT.min=250M (default)
    fname="$metrics_root/p4_filesys${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    if [[ $UseSDP -eq 1 ]]; then
        tmp_filesys_data="$metrics_root/tmp_filesys-${SDP_INSTANCE}"
    else
        tmp_filesys_data="$metrics_root/tmp_filesys"
    fi
    # Update every 60 mins
    [[ ! -f "$tmp_filesys_data" || $(find "$tmp_filesys_data" -mmin +60) ]] || return
    configurables="filesys.depot.min filesys.P4ROOT.min filesys.P4JOURNAL.min filesys.P4LOG.min filesys.TEMP.min"

    echo "" > "$tmp_filesys_data"
    for c in $configurables
    do
        $p4 configure show "$c" >> "$tmp_filesys_data"
    done
    [[ $? -ne 0 ]] && return

    rm -f "$tmpfname"
    echo "# HELP p4_filesys_min Minimum space for filesystem" >> "$tmpfname"
    echo "# TYPE p4_filesys_min gauge" >> "$tmpfname"

    for c in $configurables
    do
        configuredValue=$(egrep "$c=.*configure" $tmp_filesys_data | awk '{print $1}' | awk -F= '{print $2}')
        defaultValue=$(egrep "$c=.*default" $tmp_filesys_data | awk '{print $1}' | awk -F= '{print $2}')
        value="$configuredValue"
        [[ -z "$configuredValue" ]] && value="$defaultValue"
        # Use awk to dehumanise 1G or 500m
        bytes=$(echo "$value" | awk 'BEGIN{IGNORECASE = 1}
            function printpower(n,b,p) {printf "%u\n", n*b^p; next}
            /[0-9]$/{print $1;next};
            /K$/{printpower($1, 2, 10)};
            /M$/{printpower($1, 2, 20)};
            /G$/{printpower($1, 2, 30)};
            /T$/{printpower($1, 2, 40)};')
        # filesys.P4ROOT.min -> P4ROOT
        filesys="${c/filesys./}"
        filesys="${filesys/.min/}"
        filesys_label=",filesys=\"${filesys:-none}\""
        echo "p4_filesys_min{${serverid_label}${sdpinst_label}${filesys_label}} $bytes" >> "$tmpfname"
    done

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_versions () {
    # P4D and SDP Versions
    fname="$metrics_root/p4_version_info${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"

    p4dVersion=$(grep "Server version:" $tmp_info_data | sed -e 's/Server version: //' | sed -Ee 's/ \([0-9/]+\)//')
    p4dVersion_label=",version=\"${p4dVersion:-unknown}\""
    p4dServices=$(grep "Server services:" $tmp_info_data | sed -e 's/Server services: //')
    p4dServices_label=",services=\"${p4dServices:-unknown}\""

    rm -f "$tmpfname"
    echo "# HELP p4_p4d_build_info P4D Version/build info" >> "$tmpfname"
    echo "# TYPE p4_p4d_build_info gauge" >> "$tmpfname"
    echo "p4_p4d_build_info{${serverid_label}${sdpinst_label}${p4dVersion_label}} 1" >> "$tmpfname"
    echo "# HELP p4_p4d_server_type P4D server type/services" >> "$tmpfname"
    echo "# TYPE p4_p4d_server_type gauge" >> "$tmpfname"
    echo "p4_p4d_server_type{${serverid_label}${sdpinst_label}${p4dServices_label}} 1" >> "$tmpfname"

    if [[ $UseSDP -eq 1 && -f "/p4/sdp/Version" ]]; then
        SDPVersion=$(cat "/p4/sdp/Version")
        SDPVersion_label=",version=\"${SDPVersion:-unknown}\""
        echo "# HELP p4_sdp_version SDP Version" >> "$tmpfname"
        echo "# TYPE p4_sdp_version gauge" >> "$tmpfname"
        echo "p4_sdp_version{${serverid_label}${sdpinst_label}${SDPVersion_label}} 1" >> "$tmpfname"
    fi

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_ssl () {
    # P4D certificate
    fname="$metrics_root/p4_ssl_info${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"

    certExpiry=$(grep "Server cert expires:" $tmp_info_data | sed -e 's/Server cert expires: //')
    if [[ -z "$certExpiry" ]]; then
        return
    fi
    # Builtin date utility will parse for us
    certExpirySecs=$(date -d "$certExpiry" +%s)

    rm -f "$tmpfname"
    echo "# HELP p4_ssl_cert_expires P4D SSL certificate expiry epoch seconds" >> "$tmpfname"
    echo "# TYPE p4_ssl_cert_expires gauge" >> "$tmpfname"
    echo "p4_ssl_cert_expires{${serverid_label}${sdpinst_label}} $certExpirySecs" >> "$tmpfname"

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_change () {
    # Latest changelist counter as single counter value
    fname="$metrics_root/p4_change${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    curr_change=$($p4 counters 2>&1 | egrep '^change =' | awk '{print $3}')
    if [[ ! -z "$curr_change" ]]; then
        rm -f "$tmpfname"
        echo "# HELP p4_change_counter P4D change counter" >> "$tmpfname"
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
    if [[ $UseSDP -eq 1 ]]; then
        monfile="/tmp/mon-${SDP_INSTANCE}.out"
    else
        monfile="/tmp/mon.out"
    fi

    $p4 monitor show > "$monfile" 2> /dev/null
    rm -f "$tmpfname"
    echo "# HELP p4_monitor_by_cmd P4 running processes" >> "$tmpfname"
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

    rm -f "$tmpfname"
    echo "#HELP p4_sdp_checkpoint_log_time Time of last checkpoint log" >> "$tmpfname"
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

    # Output of logschema is:
    # ... f_field 16
    # ... f_name f_severity
    line=$($p4 logschema "$ver" | grep -B1 f_severity | head -1)
    if ! [[ $line =~ f_field ]]; then
        return
    fi
    indSeverity=$(echo "$line" | awk '{print $3}')
    indSeverity=$((indSeverity+1))    # 0->1 index
    indSS=$((indSeverity+1))
    indID=$((indSS+1))

    rm -f "$tmpfname"
    echo "#HELP p4_error_count Server errors by id" >> "$tmpfname"
    echo "#TYPE p4_error_count counter" >> "$tmpfname"
    while read count level ss_id error_id
    do
        if [[ ! -z ${ss_id:-} ]]; then
            subsystem=${subsystems[$ss_id]}
            [[ -z "$subsystem" ]] && subsystem=$ss_id
            echo "p4_error_count{${serverid_label}${sdpinst_label},subsystem=\"$subsystem\",error_id=\"$error_id\",level=\"$level\"} $count" >> "$tmpfname"
        fi
    done < <(awk -F, -v indID="$indID" -v indSS="$indSS" -v indSeverity="$indSeverity" '{printf "%s %s %s\n", $indID, $indSS, $indSeverity}' "$errors_file" | sort | uniq -c)

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_pull () {
    # p4 pull metrics - only valid for replica servers
    [[ "${P4REPLICA}" == "TRUE" ]] || return

    fname="$metrics_root/p4_pull${sdpinst_suffix}-${SERVER_ID}.prom"
    tmpfname="$fname.$$"
    if [[ $UseSDP -eq 1 ]]; then
        tmp_pull_queue="$metrics_root/pullq-${SDP_INSTANCE}.out"
    else
        tmp_pull_queue="$metrics_root/pullq.out"
    fi
    $p4 pull -l > "$tmp_pull_queue" 2> /dev/null 
    rm -f "$tmpfname"

    count=$(grep -cEa "failed\.$" "$tmp_pull_queue")
    {
        echo "# HELP p4_pull_errors P4 pull transfers failed count"
        echo "# TYPE p4_pull_errors counter"
        echo "p4_pull_errors{${serverid_label}${sdpinst_label}} $count"
    } >> "$tmpfname"

    count=$(grep -cvEa "failed\.$" "$tmp_pull_queue")
    {
        echo "# HELP p4_pull_queue P4 pull files in queue count"
        echo "# TYPE p4_pull_queue counter"
        echo "p4_pull_queue{${serverid_label}${sdpinst_label}} $count"
    } >> "$tmpfname"

#     $ p4 pull -lj
#     Current replica journal state is:       Journal 1237,  Sequence 2680510310.
#     Current master journal state is:        Journal 1237,  Sequence 2680510310.
#     The statefile was last modified at:     2022/03/29 14:15:16.
#     The replica server time is currently:   2022/03/29 14:15:18 +0000 GMT

#     $ p4 pull -lj
#     Perforce password (P4PASSWD) invalid or unset.
#     Perforce password (P4PASSWD) invalid or unset.
#     Current replica journal state is:       Journal 1237,  Sequence 2568249374.
#     Current master journal state is:        Journal 1237,  Sequence -1.
#     Current master journal state is:        Journal 0,      Sequence -1.
#     The statefile was last modified at:     2022/03/29 13:05:46.
#     The replica server time is currently:   2022/03/29 14:13:21 +0000 GMT

# perforce@gemini20:/p4 p4 -Ztag pull -lj
# ... replicaJournalCounter 12671
# ... replicaJournalNumber 12671
# ... replicaJournalSequence 17984845
# ... replicaStatefileModified 1664718313
# ... replicaTime 1664718339
# ... masterJournalNumber 12671
# ... masterJournalSequence 17985009
# ... masterJournalNumberLEOF 12671
# ... masterJournalSequenceLEOF 17984845

# perforce@gemini20:/p4 p4 -Ztag pull -lj
# Perforce password (P4PASSWD) invalid or unset.
# Perforce password (P4PASSWD) invalid or unset.
# ... replicaJournalCounter 12671
# ... replicaJournalNumber 12671
# ... replicaJournalSequence 17984845
# ... replicaStatefileModified 1664718313
# ... replicaTime 1664718339
# ... masterJournalNumber 12671
# ... masterJournalSequence -1
# ... currentJournalNumber 0
# ... currentJournalSequence -1
# ... masterJournalNumberLEOF 12671
# ... masterJournalSequenceLEOF -1
# ... currentJournalNumberLEOF 0
# ... currentJournalSequenceLEOF -1

    if [[ $UseSDP -eq 1 ]]; then
        tmp_pull_stats="$metrics_root/pull-lj-${SDP_INSTANCE}.out"
    else
        tmp_pull_stats="$metrics_root/pull-lj.out"
    fi
    $p4 -Ztag pull -lj > "$tmp_pull_stats" 2> /dev/null 

    replica_jnl_file=$(grep "replicaJournalCounter " "$tmp_pull_stats" | awk '{print $3}' )
    master_jnl_file=$(grep "masterJournalNumber " "$tmp_pull_stats" | awk '{print $3}' )
    journals_behind=$((master_jnl_file - replica_jnl_file))

    {
        echo "# HELP p4_pull_replica_journals_behind Count of how many journals behind replica is"
        echo "# TYPE p4_pull_replica_journals_behind gauge"
        echo "p4_pull_replica_journals_behind{${serverid_label}${sdpinst_label}} $journals_behind"
    }  >> "$tmpfname"

    replica_jnl_seq=$(grep "replicaJournalSequence " "$tmp_pull_stats" | awk '{print $3}' )
    master_jnl_seq=$(grep "masterJournalSequence " "$tmp_pull_stats" | awk '{print $3}' )
    {
        echo "# HELP p4_pull_replication_error Set to 1 if replication error"
        echo "# TYPE p4_pull_replication_error gauge"
    } >>  "$tmpfname"
    if [[ $master_jnl_seq -lt 0 ]]; then
        echo "p4_pull_replication_error{${serverid_label}${sdpinst_label}} 1" >> "$tmpfname"
    else
        echo "p4_pull_replication_error{${serverid_label}${sdpinst_label}} 0" >> "$tmpfname"
    fi

    {
        echo "# HELP p4_pull_replica_lag Replica lag count (bytes)"
        echo "# TYPE p4_pull_replica_lag gauge"
    } >>  "$tmpfname"

    if [[ $master_jnl_seq -lt 0 ]]; then
        echo "p4_pull_replica_lag{${serverid_label}${sdpinst_label}} -1" >> "$tmpfname"
    else
        # Ensure base 10 arithmetic used to avoid overflow errors
        lag_count=$((10#$master_jnl_seq - 10#$replica_jnl_seq))
        echo "p4_pull_replica_lag{${serverid_label}${sdpinst_label}} $lag_count" >> "$tmpfname"
    fi

    chmod 644 "$tmpfname"
    mv "$tmpfname" "$fname"
}

monitor_realtime () {
    # p4d --show-realtime - only for 2021.1 or greater
    # Intially only available for SDP
    [[ $UseSDP -eq 1 ]] || return
    p4dver=$($P4DBIN -V |grep Rev.|awk -F / '{print $3}' )
    # shellcheck disable=SC2072
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

    rm -f "$tmpfname"
    metric_count=0
    origname="rtv.db.lockwait"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime lockwait counter" >> "$tmpfname"
        echo "# TYPE $mname gauge" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    origname="rtv.db.ckp.active"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime checkpoint active indicator" >> "$tmpfname"
        echo "# TYPE $mname gauge" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    origname="rtv.db.ckp.records"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime checkpoint records counter" >> "$tmpfname"
        echo "# TYPE $mname gauge" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    origname="rtv.db.io.records"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime IO records counter" >> "$tmpfname"
        echo "# TYPE $mname counter" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    origname="rtv.rpl.behind.bytes"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime replica bytes lag counter" >> "$tmpfname"
        echo "# TYPE $mname gauge" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    origname="rtv.rpl.behind.journals"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime replica journal lag counter" >> "$tmpfname"
        echo "# TYPE $mname gauge" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    origname="rtv.svr.sessions.active"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime server active sessions counter" >> "$tmpfname"
        echo "# TYPE $mname gauge" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    origname="rtv.svr.sessions.total"
    mname="p4_${origname//./_}"
    count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
    if [[ ! -z $count ]]; then
        metric_count=$(($metric_count + 1))
        echo "# HELP $mname P4 realtime server total sessions counter" >> "$tmpfname"
        echo "# TYPE $mname counter" >> "$tmpfname"
        echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
    fi

    if [[ $metric_count -gt 0 ]]; then
        chmod 644 "$tmpfname"
        mv "$tmpfname" "$fname"
    fi
}

# Tidyup in case of errors
function tidyup() {
    # Make sure all readable by node_exporter or other user
    chmod 644 $metrics_root/*.prom
    # Delete any temp files left over
    rm -f $metrics_root/*.prom.[0-9]*
}
# Catch errors and exit (e.g. unbound variable)
trap tidyup ERR EXIT

monitor_uptime
monitor_change
monitor_processes
monitor_replicas
monitor_pull
monitor_realtime
monitor_license
monitor_filesys
monitor_versions
monitor_ssl
monitor_checkpoint
monitor_errors
