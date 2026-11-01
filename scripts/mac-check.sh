#!/usr/bin/env bash
# For the test on a real Mac: collects what can be read without
# clicking, into ~/core-starter-check-<time>.txt. Run it as the normal user
# after installing the app; once more while node and miner run. No sudo, no
# network, changes nothing. MAC_CHECK_NO_LAUNCHD=1 skips the launchd query.
set -uo pipefail
out=~/core-starter-check-$(date +%Y%m%d-%H%M%S).txt
exec > >(tee "$out") 2>&1

APP=${CORE_STARTER_APP:-/Applications/CoreStarter.app}
[[ -d $APP ]] || APP=~/Applications/"CoreStarter.app"
LABEL=info.tmmac.core-starter
GOCORE_SHA=9b16d813afd1b976059d30541a1f7c4527ddd55e616454c6c90aed56c3baf87a # scripts/build-mac.sh
SUPPORT=~/Library/"Application Support/Core Starter"
LOGS=~/Library/Logs/"CoreStarter"

h() { printf '\n== %s\n' "$1"; }
run() { printf '$ %s\n' "$*"; "$@" 2>&1 | sed 's/^/  /'; }

h "System"
sw_vers
echo "$(sysctl -n hw.model), $(sysctl -n machdep.cpu.brand_string), $(($(sysctl -n hw.memsize) >> 30)) GB, $(uname -m)"
echo "cores: $(sysctl -n hw.perflevel0.logicalcpu 2>/dev/null) performance + $(sysctl -n hw.perflevel1.logicalcpu 2>/dev/null) efficiency"
pmset -g batt | sed -n 1p
pmset -g therm 2>&1 | grep -v '^$'

h "App"
echo "path: $APP"
defaults read "$APP/Contents/Info" CFBundleShortVersionString 2>&1
run codesign --verify --deep --strict "$APP"
run codesign -dv "$APP"
run spctl --assess -vv "$APP" # Gatekeeper's verdict (rejected = unsigned build)
xattr -p com.apple.quarantine "$APP" 2>/dev/null && echo "(quarantine set: came from a download)" || echo "no quarantine flag"
sha=$(shasum -a 256 "$APP/Contents/MacOS/gocore" | cut -d' ' -f1)
[[ $sha == "$GOCORE_SHA" ]] && echo "gocore: official v2.2.2 (SHA-256 pinned)" || echo "gocore: SHA-256 $sha (not the pinned one)"

if [[ ${MAC_CHECK_NO_LAUNCHD:-0} != 1 ]]; then
  h "launchd job"
  launchctl print "gui/$(id -u)/$LABEL" 2>&1 | grep -E '^\s*(state|program|pid|last exit code|runs|path) =' | sed 's/^\s*/  /'
  ls -l ~/Library/LaunchAgents/"$LABEL".plist "$SUPPORT/$LABEL".plist 2>&1
fi

h "Processes (nice, parent)"
ps -u "$(id -u)" -o pid,ppid,nice,etime,rss,command | grep -E 'PID|core-starter|gocore|core-miner' | grep -v -e grep -e core-starter-check | cut -c1-200

h "Network"
lsof -nP -a -c gocore -iTCP -sTCP:LISTEN 2>/dev/null | sed 's/^/  /' || true
echo "gocore peers (established TCP): $(lsof -nP -a -c gocore -iTCP -sTCP:ESTABLISHED 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')"
echo "core-miner connections: $(lsof -nP -a -c core-miner -iTCP -sTCP:ESTABLISHED 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')"
echo "core-starter connections (should be 0): $(lsof -nP -a -c core-starter -i 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')"
/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate 2>&1

h "Folders and rights"
for d in "$SUPPORT" "$SUPPORT/data" "$LOGS" "$(getconf DARWIN_USER_CACHE_DIR)$LABEL"; do
  [[ -e $d ]] && stat -f '%Sp %Su %N' "$d"
done
[[ -d $SUPPORT/data ]] && {
  tmutil isexcluded "$SUPPORT/data"
  [[ -e $SUPPORT/data/.metadata_never_index ]] && echo "Spotlight: data excluded" || echo "Spotlight: data NOT excluded"
  du -sh "$SUPPORT/data" 2>/dev/null
}
df -h "$HOME" | tail -1

h "Settings"
sed -E 's/("address": *")[^"]{6}([^"]*)"/\1…\2"/' "$SUPPORT/settings.json" 2>&1
echo
cat "$SUPPORT/status.json" 2>/dev/null | head -c 1500; echo

h "Logs (end)"
tail -n 40 "$LOGS/starter.log" 2>&1
tail -n 15 "$LOGS/gocore.log" 2>&1
tail -n 15 "$LOGS/miner.log" 2>&1

printf '\nSaved: %s\n' "$out"
