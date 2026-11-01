#!/usr/bin/env bash
# For the desktop test on a real Linux machine: collects what
# can be read without clicking, into ~/core-starter-check-<time>.txt. Run it as
# the normal user, after installing the package; once more while the window is
# open (WebKit sandbox) and once while node and miner run. No sudo, no network,
# changes nothing.
set -uo pipefail
out=~/core-starter-check-$(date +%Y%m%d-%H%M%S).txt
exec > >(tee "$out") 2>&1

h() { printf '\n== %s\n' "$1"; }
run() { printf '$ %s\n' "$*"; "$@" 2>&1 | sed 's/^/  /'; }
has() { command -v "$1" >/dev/null; }

h "System"
grep -E '^(PRETTY_NAME|VERSION_ID)=' /etc/os-release
echo "kernel $(uname -r), $(uname -m)"
echo "desktop: ${XDG_CURRENT_DESKTOP:-?}, session: ${XDG_SESSION_TYPE:-?}, display: ${WAYLAND_DISPLAY:-} ${DISPLAY:-}"
ldd --version | head -1
has lspci && lspci | grep -iE 'vga|3d|display'
lsmod 2>/dev/null | grep -E '^(nvidia|amdgpu|i915|nouveau) ' | cut -d' ' -f1

h "Package"
if has dpkg-query; then
  dpkg-query -W -f='${Package} ${Version} ${Status}\n' core-starter libwebkit2gtk-4.1-0 libgtk-3-0 2>&1
else
  rpm -q core-starter webkit2gtk4.1 libwebkit2gtk-4_1-0 gtk3 2>&1 | grep -v 'not installed'
fi
ls -l /usr/lib/core-starter /usr/lib/core-starter/runtime 2>&1
run /usr/lib/core-starter/gocore version

h "Tray host (StatusNotifierWatcher)"
if has busctl; then
  busctl --user call org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus NameHasOwner s org.kde.StatusNotifierWatcher
fi
has gnome-extensions && gnome-extensions list --enabled 2>/dev/null | grep -i -E 'appindicator|tray' || true

h "WebKit sandbox prerequisites"
for k in kernel.apparmor_restrict_unprivileged_userns kernel.unprivileged_userns_clone user.max_user_namespaces; do
  v=$(sysctl -n "$k" 2>/dev/null) && echo "$k = $v"
done
if has bwrap; then ls -l "$(command -v bwrap)"; else echo "bwrap not installed"; fi
ls /etc/apparmor.d 2>/dev/null | grep -iE 'bwrap|webkit|core-starter' || true
echo "web processes (only while the window is open):"
for pid in $(pgrep -f WebKitWebProcess); do
  printf '  pid %s parent %s  userns %s (own %s)\n' "$pid" "$(ps -o comm= -p "$(ps -o ppid= -p "$pid" | tr -d ' ')")" \
    "$(readlink /proc/"$pid"/ns/user)" "$(readlink /proc/self/ns/user)"
done

h "systemd"
if has systemctl; then
  echo "user@$(id -u): $(systemctl show "user@$(id -u)" -p TimeoutStopUSec 2>&1)"
  run systemctl --user show core-starter.service -p ActiveState -p SubState -p MainPID -p KillMode -p TimeoutStopUSec -p NRestarts
  run systemctl --user is-active graphical-session.target
fi
ls -l ~/.config/autostart/core-starter.desktop 2>&1

h "Processes (nice, scheduling class)"
ps -u "$(id -u)" -o pid,ppid,ni,cls,etime,rss,comm | grep -E 'PID|core-starter|gocore|core-miner|WebKit'

h "Network (listening, firewall)"
has ss && ss -tuln 2>/dev/null | grep -E ':30300\b' || echo "port 30300 not listening"
has firewall-cmd && firewall-cmd --state 2>&1
has ufw && echo "ufw: needs sudo to read (sudo ufw status)"

h "Folders and rights"
for d in ~/.config/core-starter ~/.local/state/core-starter ~/.local/state/core-starter/logs ~/.local/share/core-starter ~/.local/share/core-starter/data "${XDG_RUNTIME_DIR:-/nonexistent}/core-starter"; do
  [[ -e $d ]] && stat -c '%A %U %n' "$d"
done
ls -l ~/.local/state/core-starter/logs 2>/dev/null
cat ~/.local/state/core-starter/webkit-stage 2>/dev/null && echo "(saved WebKit mode)"

h "Logs (end)"
tail -n 40 ~/.local/state/core-starter/logs/starter.log 2>&1
tail -n 15 ~/.local/state/core-starter/logs/gocore.log 2>&1
tail -n 15 ~/.local/state/core-starter/logs/miner.log 2>&1

printf '\nSaved: %s\n' "$out"
