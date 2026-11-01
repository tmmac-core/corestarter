#!/usr/bin/env bash
# Runs as root inside one distro container, from the folder that holds dist/
# (the packages, and dist/tests/ with the test binary and this script, put
# there by the build). Used by linux.yml (job distros) and scripts/ci-local.sh.
# Installs the package with the distro's own package manager, runs the
# integration tests as a normal user (real gocore offline, miner, window under
# Xvfb), checks that the program refuses root.
# EXPECT: 1 if the package must use its bundled runtime on this distro.
# Screenshots go to /shots.
set -euo pipefail
: "${EXPECT:?}"

if command -v apt-get >/dev/null; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq ./dist/core-starter_*_amd64.deb >/dev/null
  apt-get install -y -qq xvfb xauth dbus imagemagick procps >/dev/null
elif command -v dnf >/dev/null; then
  dnf install -y -q ./dist/core-starter-*.x86_64.rpm
  dnf install -y -q xorg-x11-server-Xvfb dbus-daemon ImageMagick procps-ng util-linux shadow-utils
else
  zypper --non-interactive --quiet install --allow-unsigned-rpm ./dist/core-starter-*.x86_64.rpm
  zypper --non-interactive --quiet install xorg-x11-server-Xvfb dbus-1 ImageMagick procps util-linux shadow
fi
ls -l /usr/lib/core-starter /usr/lib/core-starter/runtime
ldd --version | sed -n 1p # sed reads all: no SIGPIPE under pipefail

useradd -m tester
mkdir -p /shots && chmod 777 /shots
cp dist/tests/starter-linux.test /home/tester/
chmod 755 /home/tester/starter-linux.test && chown tester /home/tester/starter-linux.test
Xvfb :99 -screen 0 1280x900x24 >/dev/null 2>&1 &
sleep 2
# Containers only: the host's AppArmor blocks bubblewrap's user namespaces
# inside containers. The real desktop test runs with the sandbox.
runuser -u tester -- env HOME=/home/tester DISPLAY=:99 \
  CORE_STARTER_LINUX_IT=1 CORE_STARTER_EXPECT_RUNTIME="$EXPECT" CORE_STARTER_SHOTS=/shots \
  WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 \
  dbus-run-session -- /home/tester/starter-linux.test -test.v -test.count=1 -test.timeout 30m \
  -test.run 'Linux|SystemTexts|ProcStart|DesktopQuote|TrayIcon|XDG'

echo "== program starts and refuses root"
out=$(core-starter 2>&1 || true)
echo "$out"
echo "$out" | grep -q sudo
