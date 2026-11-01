#!/usr/bin/env bash
# Runs as root inside the systemd container (see Dockerfile). Drives the real
# core-starter.service of user "tester" and checks what the unit promises:
# a killed tray is restarted and adopts the running gocore, the miner dies
# with the tray, a package update restarts tray and gocore, a stop ends gocore
# cleanly, removing the package stops everything.
set -euo pipefail

uid=$(id -u tester)
home=/home/tester
log=$home/.local/state/core-starter/logs/starter.log
u() { runuser -u tester -- env XDG_RUNTIME_DIR=/run/user/$uid DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$uid/bus "$@"; }
fail() { echo "FAIL: $*"; exit 1; }
show_log() { echo "---- starter.log"; cat "$log" 2>/dev/null || true; }
trap show_log EXIT

# lines: wait until a line matching $1 appears after line $2 of the log ($3 s).
wait_log() {
  local pat=$1 from=$2 t=$3
  for ((i = 0; i < t; i++)); do
    if [[ -f $log ]] && tail -n +"$((from + 1))" "$log" | grep -q -- "$pat"; then return 0; fi
    sleep 1
  done
  fail "no '$pat' within ${t}s"
}
lines() { [[ -f $log ]] && wc -l <"$log" || echo 0; }
gocore_pid() { pgrep -u tester -x gocore | head -1 || true; }
miner_pid() { pgrep -u tester -x core-miner | head -1 || true; }
tray_pid() { u systemctl --user show -p MainPID --value core-starter.service; }

# docker exec comes right after the container start: wait for systemd's bus.
for ((i = 0; i < 90; i++)); do
  [[ -S /run/dbus/system_bus_socket ]] && systemctl is-system-running >/dev/null 2>&1 && break
  case $(systemctl is-system-running 2>/dev/null) in running | degraded) break ;; esac
  sleep 1
done
echo "systemd: $(systemctl is-system-running 2>&1)"
loginctl enable-linger tester
for ((i = 0; i < 30; i++)); do [[ -S /run/user/$uid/bus ]] && break; sleep 1; done
[[ -S /run/user/$uid/bus ]] || fail "no user manager for tester"
echo "user@$uid: $(systemctl show "user@$uid" -p TimeoutStopUSec)"
echo "unit: $(u systemctl --user show core-starter.service -p KillMode -p TimeoutStopUSec -p Restart | tr '\n' ' ')"

# Consent on record: node and miner on (the miner finds no pool offline and
# keeps retrying, which is enough to see it live and die).
install -d -o tester -m 0700 "$home/.config/core-starter"
cat >"$home/.config/core-starter/settings.json" <<'JSON'
{"node":true,"mine":true,"address":"cb292120d3abc3b8330a26daf4beb94b1f6a9c7b4963","worker":"ci","power":25,"autostart":false}
JSON
chown tester "$home/.config/core-starter/settings.json"

echo "== start"
u systemctl --user start core-starter.service
wait_log "started gocore pid" 0 90
sleep 15 # gocore opens its database and starts IPC
g1=$(gocore_pid); m1=$(miner_pid); t1=$(tray_pid)
[[ -n $g1 ]] || fail "no gocore"
echo "tray $t1, gocore $g1, miner ${m1:-none}"
[[ -n $m1 ]] || echo "note: no miner process at this moment (offline retry)"

echo "== tray killed hard: miner goes with it, gocore stays, restarted tray adopts it"
n=$(lines)
kill -9 "$t1"
sleep 2
[[ -z $m1 ]] || ! kill -0 "$m1" 2>/dev/null || fail "miner $m1 outlived the tray"
kill -0 "$g1" || fail "gocore $g1 died with the tray"
wait_log "adopted running gocore pid $g1" "$n" 40
[[ $(gocore_pid) == "$g1" ]] || fail "a second gocore was started"
echo "ok: adopted $g1 by tray $(tray_pid)"

# Emulated (ci-local.sh on Apple silicon): Rosetta shows the program path in
# /proc/self/exe but never " (deleted)", so update and removal cannot be seen.
emulated=${CS_EMULATED:-0}
g2=$g1
if [[ $emulated == 1 ]]; then
  echo "== package update: SKIPPED (emulated, /proc/self/exe never shows (deleted))"
else
echo "== package update (reinstall): tray restarts itself, old gocore is replaced"
n=$(lines)
dpkg -i /pkg/core-starter.deb >/dev/null
wait_log "program updated: restarting the tray" "$n" 60
wait_log "runs a replaced program file: restarting it" "$n" 60
wait_log "started gocore pid" "$n" 360
g2=$(gocore_pid)
[[ -n $g2 && $g2 != "$g1" ]] || fail "gocore not restarted after the update ($g1 -> ${g2:-none})"
[[ $(readlink "/proc/$g2/exe") != *"(deleted)"* ]] || fail "new gocore still runs a deleted file"
echo "ok: gocore $g1 -> $g2"
sleep 15
fi

echo "== systemctl stop: gocore ends cleanly through the tray"
n=$(lines)
t0=$(date +%s)
u systemctl --user stop core-starter.service
echo "stop took $(($(date +%s) - t0)) s"
tail -n +"$((n + 1))" "$log" | grep -q "gocore pid $g2 stopped" || fail "no clean stop logged"
! tail -n +"$((n + 1))" "$log" | grep -q "terminating" || fail "gocore was terminated hard"
[[ -z $(gocore_pid) && -z $(miner_pid) ]] || fail "processes left after stop"
echo "ok"

nlog=$home/.local/state/core-starter/logs/gocore.log
nlines() { [[ -f $nlog ]] && wc -l <"$nlog" || echo 0; }
repairs() { tail -n +"$(($1 + 1))" "$nlog" | grep -E 'Head state missing|Rewinding blockchain' || true; }
# start_session: user manager up (linger stands in for the login), unit
# started, gocore serving; sets G and N0 (gocore.log line before the start).
start_session() {
  loginctl enable-linger tester
  for ((i = 0; i < 30; i++)); do [[ -S /run/user/$uid/bus ]] && break; sleep 1; done
  [[ -S /run/user/$uid/bus ]] || fail "no user manager for tester"
  N0=$(nlines)
  local n
  n=$(lines)
  u systemctl --user start core-starter.service
  wait_log "started gocore pid\|adopted running gocore pid" "$n" 90
  sleep 15
  G=$(gocore_pid)
  [[ -n $G ]] || fail "no gocore"
}

echo "== logout: logind stops user@$uid (last session gone, no linger)"
start_session
n=$(lines)
t0=$(date +%s)
loginctl disable-linger tester # no session left: logind stops the user manager
for ((i = 0; i < 200; i++)); do
  ! systemctl is-active -q "user@$uid.service" && [[ -z $(gocore_pid) ]] && break
  sleep 1
done
took=$(($(date +%s) - t0))
systemctl is-active -q "user@$uid.service" && fail "user@$uid still active after ${took}s"
[[ -z $(gocore_pid) && -z $(miner_pid) ]] || fail "gocore or miner outlived the session (${took}s)"
tail -n +"$((n + 1))" "$log" | grep -q "terminated: stopping miner and gocore" || fail "tray did not see the session end"
tail -n +"$((n + 1))" "$log" | grep -q "gocore pid $G stopped" || fail "no clean stop at logout"
! tail -n +"$((n + 1))" "$log" | grep -q "terminating" || fail "gocore was terminated hard at logout"
echo "ok: session ended in ${took}s, gocore $G stopped cleanly"

echo "== login again: database as left"
start_session
r=$(repairs "$N0")
[[ -z $r ]] || fail "database repaired after a clean logout: $r"
echo "ok: gocore $G, no repair"

echo "== hard end: the user manager is killed (SIGKILL to the whole session)"
systemctl kill --signal=SIGKILL "user@$uid.service"
pkill -9 -u tester -x gocore || true
for ((i = 0; i < 30; i++)); do [[ -z $(gocore_pid) ]] && break; sleep 1; done
[[ -z $(gocore_pid) ]] || fail "gocore survived SIGKILL"
loginctl disable-linger tester
systemctl stop "user@$uid.service" 2>/dev/null || true
start_session
r=$(repairs "$N0")
echo "ok: gocore $G runs again after a hard end; repair: ${r:-none}"
u systemctl --user stop core-starter.service

if [[ $emulated == 1 ]]; then
  echo "== package removed: SKIPPED (emulated)"
  echo "ALL OK (emulated: update and removal skipped)"
  exit 0
fi
echo "== package removed: everything stops"
u systemctl --user start core-starter.service
n=$(lines)
wait_log "started gocore pid" "$n" 90
sleep 15
n=$(lines)
dpkg -r core-starter >/dev/null
wait_log "program removed" "$n" 200
for ((i = 0; i < 60; i++)); do [[ -z $(gocore_pid) && -z $(miner_pid) ]] && break; sleep 1; done
[[ -z $(gocore_pid) && -z $(miner_pid) ]] || fail "gocore or miner still running after removal"
echo "ok"
echo "ALL OK"
