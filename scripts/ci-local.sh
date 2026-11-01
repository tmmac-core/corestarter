#!/usr/bin/env bash
# The Linux CI (.github/workflows/linux.yml + runtime-pins.yml) on your own
# amd64 Linux machine with Docker, without GitHub Actions minutes. Same images
# (pulled fresh, as the runner does), same scripts:
#   build    packages + vet + unit tests in ubuntu:22.04 (oldest glibc)
#   distros  install + integration tests in the six distro containers
#   systemd  the user unit under a real systemd, container without network
#   pins     bundled glibc/GCC runtime still Ubuntu 24.04's current version
# Usage: scripts/ci-local.sh [build|distros|systemd|pins|all]   (default all)
#   IMAGES="debian:12 fedora:42"  only these distros
#   DOCKER="sudo docker"          if your user is not in the docker group
# On an arm64 machine (Apple silicon in a Lima VM with Rosetta, or arm64 Linux
# with qemu-user binfmt) every container runs as linux/amd64 through emulation.
# Network only for images, distro packages and the pinned downloads; the node
# runs offline and the miner only sees a pool on loopback, as in CI.
# Unlike CI, the systemd container gets its own cgroup namespace instead of
# the host's cgroup tree (it still needs --privileged for systemd; udev is
# masked in its image). Output: dist/ (packages), dist/ci-local/ (logs,
# screenshots). Containers are removed on exit, also after Ctrl-C.
set -euo pipefail

DOCKER=${DOCKER:-docker}
GO_IMAGE=golang:1.25.14 # same Go as linux.yml (setup-go)
ALL_IMAGES="ubuntu:22.04 debian:12 ubuntu:24.04 debian:13 fedora:42 opensuse/leap:15.6"
IMAGES=${IMAGES:-$ALL_IMAGES}

cd "$(git rev-parse --show-toplevel)"
$DOCKER info >/dev/null || { echo "docker not usable (installed? group docker? else DOCKER=\"sudo docker\")" >&2; exit 1; }
# Packages and gocore are amd64: elsewhere containers run emulated.
plat=()
if [[ $(uname -m) != x86_64 ]]; then
  plat=(--platform linux/amd64)
  [[ $($DOCKER run --rm "${plat[@]}" ubuntu:24.04 uname -m 2>/dev/null) == x86_64 ]] ||
    { echo "needs amd64 or amd64 emulation for containers (Rosetta in Lima, qemu-user binfmt)" >&2; exit 1; }
fi
git submodule update --init
out=dist/ci-local
mkdir -p "$out"

# Containers of this run, removed on any exit.
tag=cs-local-$$
ctx=
cleanup() {
  local ids
  ids=$($DOCKER ps -aq --filter "label=$tag" 2>/dev/null || true)
  [[ -z $ids ]] || $DOCKER rm -f $ids >/dev/null 2>&1 || true
  [[ -z $ctx ]] || rm -rf "$ctx"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# As in the matrix of linux.yml: which distros must use the bundled runtime.
expect() { case $1 in ubuntu:22.04 | debian:12) echo 1 ;; *) echo 0 ;; esac; }

build() {
  echo "== build (log: $out/build.log)"
  local go=.cache/linux/go-${GO_IMAGE#golang:} c
  if [[ ! -x $go/bin/go ]]; then
    c=$($DOCKER create "${plat[@]}" --label "$tag" "$GO_IMAGE")
    rm -rf "$go.tmp" && mkdir -p "$go.tmp"
    $DOCKER cp "$c:/usr/local/go/." "$go.tmp"
    rm -rf "$go" && mv "$go.tmp" "$go"
  fi
  $DOCKER run --rm "${plat[@]}" --pull always --label "$tag" -v "$PWD:/src" -w /src \
    -e PATH="/src/$go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    -e GOPATH=/src/.cache/linux/gopath -e GOCACHE=/src/.cache/linux/gocache -e GOFLAGS=-modcacherw \
    -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
    ubuntu:22.04 bash -c '
      trap "chown -R $HOST_UID:$HOST_GID dist .cache" EXIT
      set -euo pipefail
      ./scripts/linux-build-deps.sh
      git config --global --add safe.directory /src
      git config --global --add safe.directory /src/third_party/RandomY
      ./scripts/build-linux.sh
      go vet .
      go vet -tags nominer .
      go test -race -count=1 . ./internal/...
      go test -c -o dist/tests/starter-linux.test .
      cp ci/distro-test.sh dist/tests/
    ' >"$out/build.log" 2>&1 || { tail -n 40 "$out/build.log"; return 1; }
  ls dist/core-starter*
}

# Like the runner: an idle container, the test via docker exec (so PID 1 is
# not the test shell), screenshots copied out afterwards.
distros() {
  local img name c rc failed=() imgs
  read -r -a imgs <<<"$IMAGES"
  [[ ${#imgs[@]} -gt 0 ]] || { echo "IMAGES is empty" >&2; return 1; }
  for img in "${imgs[@]}"; do
    name=${img//[:\/]/-}
    echo "== $img (log: $out/$name.log)"
    rm -rf "$out/shots-$name"
    c=$($DOCKER run -d "${plat[@]}" --pull always --label "$tag" --security-opt seccomp=unconfined \
      -v "$PWD/dist:/w/dist:ro" "$img" tail -f /dev/null)
    rc=0
    $DOCKER exec -w /w -e EXPECT="$(expect "$img")" "$c" bash dist/tests/distro-test.sh >"$out/$name.log" 2>&1 || rc=$?
    $DOCKER cp "$c:/shots" "$out/shots-$name" >/dev/null 2>&1 || true
    $DOCKER rm -f "$c" >/dev/null
    if [[ $rc -eq 0 ]]; then echo "   ok"; else echo "   FAILED"; tail -n 30 "$out/$name.log"; failed+=("$img"); fi
  done
  [[ ${#failed[@]} -eq 0 ]] || { echo "failed: ${failed[*]}"; return 1; }
}

systemd() {
  echo "== systemd (log: $out/systemd.log)"
  local c rc=0
  ctx=$(mktemp -d)
  mkdir -p "$ctx/dist" "$ctx/ci/systemd"
  cp dist/core-starter_*_amd64.deb "$ctx/dist/"
  cp ci/systemd/Dockerfile ci/systemd/test.sh "$ctx/ci/systemd/"
  $DOCKER build -q "${plat[@]}" --pull --no-cache -t cs-systemd -f "$ctx/ci/systemd/Dockerfile" "$ctx" >/dev/null
  # Emulated: the system services' x86-64 syscall filter and their W^X rule
  # cannot hold under the translator (journald, logind crash with SIGTRAP).
  # Our own unit uses neither, so what is tested stays the same.
  local emu=()
  if [[ ${#plat[@]} -gt 0 ]]; then
    printf '[Service]\nSystemCallFilter=\nSystemCallArchitectures=\nMemoryDenyWriteExecute=no\n' >"$ctx/emulated.conf"
    emu=(-v "$ctx/emulated.conf:/etc/systemd/system/service.d/10-emulated.conf:ro")
  fi
  c=$($DOCKER run -d "${plat[@]}" "${emu[@]}" --label "$tag" --privileged --cgroupns=private \
    --tmpfs /run --tmpfs /run/lock --network none cs-systemd)
  $DOCKER exec -e CS_EMULATED=$((${#plat[@]} > 0)) "$c" /ci/test.sh >"$out/systemd.log" 2>&1 || rc=$?
  $DOCKER rm -f "$c" >/dev/null
  if [[ $rc -eq 0 ]]; then echo "   ok"; else echo "   FAILED"; tail -n 40 "$out/systemd.log"; return 1; fi
}

pins() {
  echo "== pins"
  $DOCKER run --rm "${plat[@]}" --pull always --label "$tag" -v "$PWD:/src:ro" -w /src ubuntu:24.04 bash ci/runtime-pins.sh
}

case ${1:-all} in
  build) build ;;
  distros) distros ;;
  systemd) systemd ;;
  pins) pins ;;
  all) build && distros && systemd && pins ;;
  *) echo "usage: $0 [build|distros|systemd|pins|all]" >&2; exit 2 ;;
esac
echo "done: $(git rev-parse --short HEAD)$([[ -z $(git status --porcelain) ]] || echo ' (with local changes)')"
