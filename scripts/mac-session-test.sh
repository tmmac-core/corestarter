#!/bin/bash
# Simulates the end of a macOS session for CoreStarter without logging out.
#
# The E2E test build (E2E=1 scripts/build-mac.sh: offline node, no peers) runs
# as its own launchd job next to a real installation: label
# info.tmmac.core-starter.sessiontest, same ExitTimeOut as the shipped job, all
# folders below one test folder (CORE_STARTER_E2E_ROOT). The chain is an APFS
# clone of a real data folder, so gocore opens a real database.
#
#   A  launchctl bootout: what launchd does to the job at logout (SIGTERM, up
#      to ExitTimeOut, then SIGKILL). Must end gocore cleanly, in time, and
#      the next start must find the database as it was left.
#   B  the system cuts the grace short: SIGTERM to the helper, SIGKILL to
#      helper and gocore after CUT seconds. gocore must start again; any
#      repair is reported.
#   C  gocore killed while running: the helper of a new job adopts nothing,
#      starts gocore again, the database opens.
#
# Usage: scripts/mac-session-test.sh [data folder]   (default: the installed
# app's chain in ~/Library/Application Support/Core Starter/data; it must not
# be in use). Nothing outside the test folder is written.
set -euo pipefail

cd "$(dirname "$0")/.."
APP=${CORE_STARTER_APP:-$PWD/dist/CoreStarter.app}
HELPER="$APP/Contents/Library/LoginItems/CoreStarter Helper.app/Contents/MacOS/core-starter"
GOCORE="$APP/Contents/MacOS/gocore"
SRC=${1:-$HOME/Library/Application Support/Core Starter/data}
CUT=${CUT:-3}
LABEL=info.tmmac.core-starter.sessiontest
DOMAIN=gui/$(id -u)
T=/tmp/csst # short: the IPC socket path must stay under 104 bytes
ROOT="$T/CoreStarter"
LOG=$T/logs/starter.log
NLOG=$T/logs/gocore.log
PLIST=$T/$LABEL.plist

fail() { echo "FAIL: $*"; exit 1; }
lines() { [[ -f $1 ]] && wc -l <"$1" | tr -d ' ' || echo 0; }
# wait_log file pattern from-line seconds
wait_log() {
  local i
  for ((i = 0; i < $4; i++)); do
    if [[ -f $1 ]] && tail -n +"$(($3 + 1))" "$1" | grep -q -- "$2"; then return 0; fi
    sleep 1
  done
  fail "no '$2' in $(basename "$1") within $4 s"
}
gocore_pids() { pgrep -f "^$GOCORE .*--datadir $ROOT/data" || true; }
helper_pid() { pgrep -f "^$HELPER --tray" || true; }
# heads of the last gocore run: header/full/fast block numbers it loaded
heads() {
  local from=$1
  tail -n +"$((from + 1))" "$NLOG" | grep -o 'Loaded most recent local [a-z]* block.*number=[0-9,]*\|Loaded most recent local header.*number=[0-9,]*' |
    sed -E 's/Loaded most recent local ([a-z]+( block)?).*number=([0-9,]+)/\1=\3/' | tr '\n' ' '
}
repairs() { tail -n +"$(($1 + 1))" "$NLOG" | grep -E 'Head state missing|Rewinding blockchain|Rewound|repair' || true; }

cleanup() {
  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
  for p in $(gocore_pids); do kill -9 "$p" 2>/dev/null || true; done
}
trap 'cleanup; echo "---- starter.log (tail)"; tail -20 "$LOG" 2>/dev/null || true' EXIT

[[ -x $HELPER ]] || fail "$HELPER missing: run E2E=1 scripts/build-mac.sh"
grep -aq CORE_STARTER_E2E_ROOT "$HELPER" || fail "not an E2E build (CORE_STARTER_E2E_ROOT unknown): E2E=1 scripts/build-mac.sh"
[[ -d $SRC/gocore ]] || fail "no chain in $SRC"
if pgrep -f "^.*/gocore .*--datadir $SRC" >/dev/null; then fail "$SRC is in use by a running gocore"; fi
launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1 && fail "$LABEL is still loaded (launchctl bootout $DOMAIN/$LABEL)"

echo "== test folder $T (APFS clone of $(du -sh "$SRC" | cut -f1) chain)"
rm -rf "$T"
mkdir -p "$ROOT" "$T/logs" "$T/run"
chmod 700 "$T" "$ROOT" "$T/run"
cp -cR "$SRC" "$ROOT/data"
cat >"$ROOT/settings.json" <<'JSON'
{"node":true,"mine":false,"address":"","worker":"sessiontest","power":25,"autostart":false,"lang":"en"}
JSON
chmod 600 "$ROOT/settings.json"

# Same keys as jobPlist (autostart_darwin.go), own label and environment.
esc() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' <<<"$1"; }
cat >"$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>$LABEL</string>
	<key>ProgramArguments</key>
	<array><string>$(esc "$HELPER")</string><string>--tray</string></array>
	<key>EnvironmentVariables</key><dict><key>CORE_STARTER_E2E_ROOT</key><string>$T</string></dict>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
	<key>LimitLoadToSessionType</key><string>Aqua</string>
	<key>ProcessType</key><string>Standard</string>
	<key>ExitTimeOut</key><integer>120</integer>
	<key>ThrottleInterval</key><integer>30</integer>
	<key>StandardOutPath</key><string>/dev/null</string>
	<key>StandardErrorPath</key><string>/dev/null</string>
</dict>
</plist>
EOF
plutil -lint "$PLIST" >/dev/null

# start_job: load the job, wait until gocore runs and serves IPC; sets G, H, N0.
start_job() {
  local n
  n=$(lines "$LOG")
  N0=$(lines "$NLOG")
  launchctl bootstrap "$DOMAIN" "$PLIST"
  wait_log "$LOG" "started gocore pid\|adopted running gocore pid" "$n" 120
  # Ready: gocore opened its IPC endpoint (awaitReady does the same).
  local i
  for ((i = 0; i < 300; i++)); do [[ -S $T/run/gocore.ipc ]] && break; sleep 1; done
  [[ -S $T/run/gocore.ipc ]] || fail "gocore IPC not up after 300 s"
  sleep 10 # signal handler is installed right after IPC; let the node settle
  G=$(gocore_pids | head -1)
  H=$(helper_pid)
  [[ -n $G && -n $H ]] || fail "helper ${H:-none}, gocore ${G:-none}"
  echo "   helper $H, gocore $G; loaded: $(heads "$N0")"
}

# Start once and stop normally: the reference heads.
echo "== reference run"
start_job
REF=$(heads "$N0")

echo "== A: launchctl bootout (logout path: SIGTERM, ExitTimeOut 120 s)"
n=$(lines "$LOG")
t0=$(date +%s)
launchctl bootout "$DOMAIN/$LABEL"
# bootout returns when the job is gone (or after ExitTimeOut + SIGKILL);
# gocore runs in its own process group and may outlive the helper.
for ((i = 0; i < 300; i++)); do [[ -z $(gocore_pids) && -z $(helper_pid) ]] && break; sleep 1; done
took=$(($(date +%s) - t0))
[[ -z $(helper_pid) ]] || fail "helper still running"
[[ -z $(gocore_pids) ]] || fail "gocore still running $took s after bootout"
tail -n +"$((n + 1))" "$LOG" | grep -q "terminated: stopping miner and gocore\|SIGTERM\|signal: terminated" || fail "helper did not log the session end"
tail -n +"$((n + 1))" "$LOG" | grep -q "gocore pid $G stopped" || fail "no clean gocore stop logged"
! tail -n +"$((n + 1))" "$LOG" | grep -q "terminating" || fail "gocore was terminated hard"
[[ $took -lt 120 ]] || fail "stop took $took s, launchd kills the helper at 120 s"
echo "   ok: gocore $G stopped cleanly, $took s after bootout"
A_TOOK=$took

echo "   restart after A"
start_job
r=$(repairs "$N0")
[[ -z $r ]] || fail "database repaired after a clean stop: $r"
[[ $(heads "$N0") == "$REF" ]] || echo "   note: heads moved: $REF -> $(heads "$N0")"
echo "   ok: no repair"

echo "== B: grace cut to $CUT s (SIGTERM, then SIGKILL to helper and gocore)"
n=$(lines "$LOG")
kill -TERM "$H"
sleep "$CUT"
gp=$(gocore_pids)
kill -9 "$H" 2>/dev/null || true
for p in $gp; do kill -9 "$p" 2>/dev/null || true; done
launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
sleep 2
[[ -z $(gocore_pids) ]] || fail "gocore survived SIGKILL"
if [[ -n $gp ]]; then echo "   gocore $gp was still running after $CUT s and was killed"; else echo "   gocore had already stopped within $CUT s"; fi
start_job
B_REPAIR=$(repairs "$N0")
echo "   restart after B: loaded $(heads "$N0"); repair: ${B_REPAIR:-none}"

echo "== C: gocore killed while running, helper stays"
n=$(lines "$LOG")
kill -9 "$G"
wait_log "$LOG" "gocore pid $G exited" "$n" 30
N0=$(lines "$NLOG")
wait_log "$LOG" "started gocore pid" "$n" 60
# Not the IPC socket: the killed gocore left its file behind.
wait_log "$NLOG" "Loaded most recent local full block" "$N0" 300
G2=$(gocore_pids | head -1)
[[ -n $G2 && $G2 != "$G" ]] || fail "no new gocore after the kill"
C_REPAIR=$(repairs "$N0")
echo "   ok: helper restarted gocore ($G -> ${G2:-?}); loaded $(heads "$N0"); repair: ${C_REPAIR:-none}"

echo "== final stop (bootout)"
n=$(lines "$LOG")
launchctl bootout "$DOMAIN/$LABEL"
for ((i = 0; i < 300; i++)); do [[ -z $(gocore_pids) ]] && break; sleep 1; done
tail -n +"$((n + 1))" "$LOG" | grep -q "stopped" || fail "no clean stop at the end"
echo
echo "RESULT: A clean in ${A_TOOK} s; B repair: ${B_REPAIR:-none}; C repair: ${C_REPAIR:-none}"
echo "ALL OK"
