#!/bin/bash
#==============================================================================
# Copyright and license info is available in the LICENSE file included with
# this package, and also available online:
# https://swarm.workshop.perforce.com/view/guest/perforce_software/helix-installer/main/LICENSE
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Declarations
declare Version=1.6.0
declare ResetTarget=/hxdepots
declare DownloadsDir=$ResetTarget/downloads
declare BackupDir=Unset
declare BackupFile=
declare TmpFile=/tmp/tmp.csd4sdp.$$.$RANDOM
declare RunUser=perforce
declare ThisUser=
declare CBIN=/p4/common/bin
declare ThisScript=${0##*/}
declare SDPInstance=Unset
declare PasswordFile=

#------------------------------------------------------------------------------
# Function: usage (required function)
#
# Input:
# $1 - style, either -h (for short form) or -man (for man-page like format).
#------------------------------------------------------------------------------
function usage {
   declare style=${1:--h}

   echo "USAGE for $ThisScript v$Version:

$ThisScript -i <sdp_instance> [-d <data_dir>] [-u <osuser>]

or

$ThisScript [-h|-man]
"
   if [[ $style == -man ]]; then
      echo -e "
DESCRIPTION:
	This script transforms a stock Sample Depot instance into
	one that works with the SDP.

REQUIREMENTS:
	A P4D process must be live and running with the stock
	Sample Depot data set, on a sport 

ARGUMENTS:
 -i <sdp_instance>
	Specify the SDP Instance in which the Sample Depot data set is
	running.  This argument is required.

 -d <data_dir>
	Specify the data directory where supporting files exist, such as the
	*.p4s data files used by this script.

 -u <osuser>
	Specify the Linux operating system user account under which p4d runs.
	If omitted, the default is 'perforce'.

 -D     Set extreme debugging verbosity.

HELP OPTIONS:
 -h	Display short help message
 -man	Display man-style help message

EXAMPLES:
	Usage to configure Instance 1:
	cd /where/this/script/is
	$ThisScript 1 2>&1 | tee log.${ThisScript%.sh}.1

	Usage to configure Instance abc:
	cd /where/this/script/is
	$ThisScript abc 2>&1 | tee log.${ThisScript%.sh}.abc
"
   fi

   exit 1
}

#------------------------------------------------------------------------------
# Function bail().
# Sample Usage:
#    bail "Missing something important. Aborting."
#    bail "Aborting with exit code 3." 3
function bail () { echo -e "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

#------------------------------------------------------------------------------
# Functions.  The runCmd() function is similar to functions defined in SDP core
# libraries, but we need to duplicate them here since this script runs before
# the SDP is available on the machine (and we want no dependencies for this
# script.
function runCmd {
   declare cmd=${1:-echo Testing runCmd}
   declare desc=${2:-""}

   declare cmdToShow=$cmd

   [[ "$cmdToShow" == *"<"* ]] && cmdToShow=${cmdToShow%%<*}
   [[ "$cmdToShow" == *">"* ]] && cmdToShow=${cmdToShow%%>*}

   [[ -n "$desc" ]] && echo "$desc"
   echo "Running: $cmdToShow"
   if [[ $NoOp -eq 0 ]]; then
      $cmd
   else
      echo "NO-OP: Would run: $cmdToShow"
   fi
   return $?
}

#==============================================================================
# Command Line Processing

declare -i NoOp=0
declare -i shiftArgs=0
declare DataDir="$PWD"

set +u

while [[ $# -gt 0 ]]; do
   case $1 in
      (-i) SDPInstance=$2; shiftArgs=1;;
      (-d) DataDir="$2"; shiftArgs=1;;
      (-u) RunUser="$2"; shiftArgs=1;;
      (-n) NoOp=1;;
      (-h) usage -h;;
      (-man) usage -man;;
      (-D) set -x;; # Debug; use 'set -x' mode.
   esac

   # Shift (modify $#) the appropriate number of times.
   shift; while [[ $shiftArgs -gt 0 ]]; do
      [[ $# -eq 0 ]] && bail "Usage Error: Wrong numbers of args or flags to args."
      shiftArgs=$shiftArgs-1
      shift
   done
done
set -u

#------------------------------------------------------------------------------
# Usage Validation

[[ $SDPInstance == Unset ]] && \
   bail "Bad Usage: The '<sdp_instance>' argument is required."

[[ ! -r $CBIN/p4_vars ]] && \
   bail "Missing SDP Environment File [$CBIN/p4_vars]. Aborting."

#------------------------------------------------------------------------------
# Main Program

ThisUser=$(whoami)

if [[ "$ThisUser" != "$RunUser" ]]; then
   bail "Run as $RunUser, not $ThisUser."
else
   echo Verified: Running as user $RunUser.
fi

# Load SDP environment and variable definitions.
# shellcheck disable=SC1090
source "$CBIN/p4_vars" "$SDPInstance" ||\
   bail "Failed to load SDP environment. Aborting."

export P4ENVIRO=/dev/null/.p4enviro
export P4CONFIG=.p4config

PasswordFile=$P4CCFG/.p4passwd.${P4SERVER}.admin

cd "$ResetTarget/sdp/Server/setup" ||\
   bail "Failed to cd to [$ResetTarget/sdp/Server/setup]."

echo "Operating in SDP server setup area [$PWD]."

runCmd "$P4BIN -u bruno -s info -s" "Verifying server is offline." &&\
   bail "Perforce server is unexpectedly online. Aborting."

runCmd "/p4/${SDPInstance}/bin/p4d_${SDPInstance} -jr $DownloadsDir/PerforceSample/checkpoint" \
   "Loading the Sample Depot metadata in instance ${SDPInstance}." ||\
   bail "Failed to load Sample Depot checkpoint."

runCmd "/p4/${SDPInstance}/bin/p4d_${SDPInstance} -xu" \
   "Upgrading databases (p4d -xu) for instance ${SDPInstance}." ||\
   bail "Failed to upgrade databases."

runCmd "/p4/${SDPInstance}/bin/p4d_${SDPInstance} -xi" \
   "Enabling unicode mode (p4d -xi) for instance ${SDPInstance}." ||\
   bail "Failed to enable unicode mode"

if [[ $P4PORT == "ssl:"* ]]; then
   runCmd "/p4/${SDPInstance}/bin/p4d_${SDPInstance} -Gc" \
      "Generating OpenSSL Certificates for instance $SDPInstance." ||\
      bail "Failed to generate OpenSSL Certs for Instance $SDPInstance."
fi

if [[ $NoOp -eq 0 ]]; then
   echo "Starting services p4broker_${SDPInstance}_init and p4d_${SDPInstance}_init."
   "/p4/${SDPInstance}/bin/p4broker_${SDPInstance}_init" start < /dev/null > /dev/null 2>&1 &
   "/p4/${SDPInstance}/bin/p4d_${SDPInstance}_init" start < /dev/null > /dev/null 2>&1 &
   sleep 1
else
   echo "NO-OP: Would start services p4broker_${SDPInstance}_init and p4d_${SDPInstance}_init."
fi

if [[ $P4PORT == "ssl:"* ]]; then
   # Note: Automating a 'p4 trust -y' (especially with '-f') is TOTALLY
   # INAPPROPRIATE in any production environment, as it defeats the purpose of the
   # Open SSL trust mechanism.  But for our purposes here, where scripts spin up
   # throw-away data sets for testing or training purposes, it's just dandy.
   runCmd "/p4/${SDPInstance}/bin/p4_${SDPInstance} -p $P4PORT trust -y -f" \
      "Trusting the OpenSSL Cert of the server." ||\
      bail "Failed to trust the server."
   runCmd "/p4/${SDPInstance}/bin/p4_${SDPInstance} -p $P4BROKERPORT trust -y -f" \
      "Trusting the OpenSSL Cert of the broker." ||\
      bail "Failed to trust the broker."
fi

runCmd "$P4BIN -u bruno -s info -s" "Verifying direct connection to Perforce server." ||\
   bail "Could not connect to Perforce server."

runCmd "$P4BIN -u bruno -s -p $P4BROKERPORT info -s" "Verifying via-broker connection to Perforce server." ||\
   bail "Could not connect to Perforce server via broker."

[[ "$($P4BIN -u bruno protects -m)" == super ]] ||\
   bail "Could not verify super user access for $P4USER on port $P4PORT.  Is this the Sample depot? Aborting."

echo "Super user access for bruno verified."

if [[ $NoOp -eq 0 ]]; then
   echo "Creating user $P4USER."
   sed "s:__EDITME_ADMIN_P4USER__:$P4USER:g" "$DataDir/admin.user.p4s" > "$TmpFile"
   "$P4BIN" -u bruno user -f -i < "$TmpFile"

   echo "Adding user to NoTicketExpiration group."
   sed "s:__EDITME_ADMIN_P4USER__:$P4USER:g" "$DataDir/NoTicketExpiration.group.p4s" > "$TmpFile"
   "$P4BIN" -u bruno group -i < "$TmpFile"

   echo "Promoting user $P4USER to super user."
   "$P4BIN" -u bruno protect -o > "$TmpFile"
   echo -e "\tsuper user $P4USER * //...\n" >> "$TmpFile"
   "$P4BIN" -u bruno protect -i < "$TmpFile"
else
   echo "NO-OP: Would create $P4USER as a super user."
fi

cat "$PasswordFile" > "$TmpFile"
cat "$PasswordFile" >> "$TmpFile"

"$P4BIN" -u bruno passwd "$P4USER" < "$TmpFile"

runCmd "/p4/common/bin/p4login" "Logging in $P4USER super user." ||\
   bail "Failed to login super user $P4USER. Aborting."

# Variable     Format                              Sample Values
# P4PORT       [ssl:]<P4DPortNum>                  ssl:1999, 1999
# P4BROKERPORT [ssl:]<BrokerPort>                  ssl:1666, 1666
for p in $P4PORT $P4BROKERPORT; do
   if [[ $p == "ssl:"* ]]; then
      runCmd "$P4BIN -p $p trust -y" "Trusting P4PORT=$p." ||\
         bail "Failed to trust P4PORT=$p."
   fi
   cmd="$P4BIN -u $P4USER -p $p login -a"
   echo "Running: $cmd < $PasswordFile"
   $cmd < "$PasswordFile" ||\
      bail "Login as perforce using P4PORT=$p failed.  Aborting."
done

runCmd "cat $P4TICKETS" "Showing P4TICKETS:"

runCmd "mv configure_new_server.sh configure_new_server.sh.orig" \
   "Tweaking configure_new_server.sh settings to values more appropriate for a demo-grade installation, e.g. reducing 5G storage limits." ||\
   bail "Failed to move configure_new_server.sh to configure_new_server.sh.orig."

# Warning: If the values in configure_new_server.sh are changed from 5G, this
# will need to be updated.
sed -e 's/filesys.P4ROOT.min=5G/filesys.P4ROOT.min=10M/g' \
   -e 's/filesys.depot.min=5G/filesys.depot.min=10M/g' \
   -e 's/filesys.P4JOURNAL.min=5G/filesys.P4JOURNAL.min=10M/g' \
   configure_new_server.sh.orig >\
   configure_new_server.sh ||\
   bail "Failed to do sed substitutions in $ResetTarget/sdp/Server/setup/configure_new_server.sh.orig."

runCmd "chmod +x configure_new_server.sh"

echo "Changes made to configure_new_server.sh:"
diff configure_new_server.sh.orig configure_new_server.sh

runCmd "./configure_new_server.sh $SDPInstance" \
   "Applying SDP configurables." ||\
   bail "Failed to set SDP configurables. Aborting."

for depot in $(/bin/ls -d $ResetTarget/downloads/PerforceSample/*); do
   [[ $depot == *"checkpoint"* ]] && continue
   [[ $depot == *"README"* ]] && continue
   [[ $depot == *"readme"* ]] && continue
   if [[ $depot == *"spec"* ]]; then
      runCmd "/usr/bin/rsync -a $depot/ /p4/$SDPInstance/depots/${depot##*/}" \
         "Copying Sample Depot archive files for spec depot [${depot##*/}]." ||\
         echo -e "\nWarning: Non-zero exit code $? from rsync for depot ${depot##*/}."
   else
      runCmd "/usr/bin/rsync -a --delete $depot/ /p4/$SDPInstance/depots/${depot##*/}" \
         "Copying Sample Depot archive files for depot [${depot##*/}]." ||\
         echo -e "\nWarning: Non-zero exit code $? from rsync for depot ${depot##*/}."
   fi
done

runCmd "$P4BIN admin updatespecdepot -a" \
   "Updating spec depot." || bail "Failed to udpate spec depot. Aborting."

runCmd "/usr/bin/rsync -a /p4/$SDPInstance/root/spec/ /p4/$SDPInstance/depots/spec" \
   "Copying a few spec depot files." ||\
   echo -e "\nWarning: Non-zero exit code $? from rsync for spec depot."

runCmd "/bin/rm -rf /p4/$SDPInstance/root/spec" \
   "Cleanup redundant copy of spec depot files." ||:

runCmd "/p4/common/bin/live_checkpoint.sh $SDPInstance" \
   "Taking Live Checkpoint." || bail "Live checkpoint failed. Aborting."

[[ $BackupDir == Unset ]] && BackupDir=/p4/$SDPInstance/backup

if [[ -d $BackupDir ]]; then
    runCmd "/bin/rm -rf $BackupDir" \
       "Removing old backup dir [$BackupDir]."
fi

if [[ ! -d $BackupDir ]]; then
   runCmd "/bin/mkdir -p $BackupDir" \
      "Creating new empty backups directory: $BackupDir." ||\
      bail "Failed to create backups dir [$BackupDir]. Aborting."
fi

BackupFile=$BackupDir/p4_$SDPInstance.backup.$(date +'%Y-%m-%d-%H%M').tgz
LastCheckpoint=$(ls -1 -t /p4/"$SDPInstance"/checkpoints/p4_"${SDPInstance}".ckp.*.gz 2>/dev/null)
BackupPaths="/p4/${SDPInstance}/depots"
[[ -n "$LastCheckpoint" ]] && BackupPaths="$BackupPaths $LastCheckpoint"

runCmd "tar -czf $BackupFile $BackupPaths" \
   "Creating backup $BackupFile." ||\
   bail "Failed to backup instance $SDPInstance. Aborting."

echo -e "\nSUCCESS:  SDP Instance $SDPInstance loaded with sample depot data, live checkpoint done, and backup created.  Good to go!\n"

exit 0
