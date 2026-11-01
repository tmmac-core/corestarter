#!/usr/bin/env bash
# Builds the Linux packages: dist/core-starter_<version>_amd64.deb, .rpm and
# .tar.gz (base for the AUR package). Run it in ubuntu:22.04 (CI does), so our
# own binaries need nothing newer than glibc 2.35; tools/elfimports checks that.
#
# 1. Downloads the official gocore release and the glibc runtime it needs on
#    systems older than its build (Ubuntu 24.04 packages from Launchpad, whose
#    file URLs stay valid), verifies every file against the SHA-256 pinned below.
# 2. Builds core-miner (RandomY, pinned submodule, static C++ runtime) and the
#    starter (cgo: GTK 3 + WebKitGTK 4.1).
# 3. Stages /usr/lib/core-starter and the licences, packs deb/rpm with nfpm.
#
# Needs: go, gcc/g++, cmake, curl, pkg-config, libgtk-3-dev,
# libwebkit2gtk-4.1-dev (scripts/linux-build-deps.sh installs them).
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.25.14 # same compiler on every build host
export LC_ALL=C

GOCORE_VERSION=v2.2.2
GOCORE_URL=https://github.com/core-coin/go-core/releases/download/$GOCORE_VERSION/gocore-linux-x86_64
GOCORE_SHA=f26ee3ee3615b4ffd285644b0bdc96c14c839d220ed0e88a80831b29d0d93e79
GOCORE_LICENSE_URL=https://raw.githubusercontent.com/core-coin/go-core/$GOCORE_VERSION/LICENSE
GOCORE_LICENSE_SHA=e2cbe1260d2307e00c04cdc94c85c9e254dde6a17281799a92b8a557d8db1812

# glibc runtime for gocore on systems older than its build (Ubuntu 22.04,
# Debian 12): ld.so, libc and libm from one libc6 package, libstdc++ and
# libgcc_s from one GCC (gcc-14-base only for their licence text). Ubuntu 24.04 (noble) packages, 2026-10-22.
LP=https://launchpad.net/ubuntu/+archive/primary/+files
RUNTIME_PKGS=(
  "libc6_2.39-0ubuntu8.9_amd64.deb ff5557d99b51f761c4b7c92368b9cc45565eda17df9bf9eb4b134d09825008be"
  "libstdc++6_14.2.0-4ubuntu2~24.04.1_amd64.deb a51f8de7829211db961a31f02158058ad1a95f92ac6d0a5dff6350e2821c54c0"
  "libgcc-s1_14.2.0-4ubuntu2~24.04.1_amd64.deb aa7fadbe33b78bcf99885318040601c550c208929565b179891d9a3cc2aa68cd"
  "gcc-14-base_14.2.0-4ubuntu2~24.04.1_amd64.deb b95c172411a7fdae70307cf33a9f5320ba5e056b556454543dd5b679d5ce1c4f"
)
RANDOMY_COMMIT=c9d185a055b7604e8d58059031e0e33dfe577cc5 # core-coin/RandomY v1.1.17
NFPM=github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0
MAX_GLIBC=2.35
NOMINER=${NOMINER:-0}

grep -q "const gocoreVersion = \"$GOCORE_VERSION\"" gocore_linux.go ||
  { echo "gocore_linux.go does not say $GOCORE_VERSION" >&2; exit 1; }
VERSION=$(sed -n 's/^\tappVersion *= *"\(.*\)"/\1/p' app.go)
[[ -n $VERSION ]] || { echo "appVersion not found in app.go" >&2; exit 1; }

CACHE=.cache/linux
STAGE=$CACHE/stage
LIB=$STAGE/lib
DOC=$STAGE/doc
rm -rf "$STAGE"
mkdir -p "$CACHE" "$LIB/runtime" "$DOC" dist

sha() { sha256sum "$1" | cut -d' ' -f1; }
fetch() { # url dest sha
  if [[ ! -f $2 || $(sha "$2") != "$3" ]]; then
    # Launchpad answers 503 now and then (27.10.: two of four files).
    curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 -o "$2.part" "$1"
    mv "$2.part" "$2"
  fi
  [[ $(sha "$2") == "$3" ]] || { echo "SHA-256 mismatch: $2" >&2; exit 1; }
}

echo "== gocore $GOCORE_VERSION"
fetch "$GOCORE_URL" "$CACHE/gocore-$GOCORE_VERSION" "$GOCORE_SHA"
install -m 0755 "$CACHE/gocore-$GOCORE_VERSION" "$LIB/gocore"
fetch "$GOCORE_LICENSE_URL" "$CACHE/LICENSE-gocore-$GOCORE_VERSION.txt" "$GOCORE_LICENSE_SHA"
cp "$CACHE/LICENSE-gocore-$GOCORE_VERSION.txt" "$DOC/LICENSE-gocore.txt"
cp third_party/licenses/LGPL-3.0.txt "$DOC/LICENSE-gocore-LGPL3.txt" # go-core's file headers (NOTICE.md)

echo "== glibc runtime (only used where the system's glibc is too old)"
X=$CACHE/x
rm -rf "$X" && mkdir -p "$X"
for entry in "${RUNTIME_PKGS[@]}"; do
  read -r pkg want <<<"$entry"
  fetch "$LP/${pkg//\~/%7E}" "$CACHE/$pkg" "$want"
  dpkg-deb -x "$CACHE/$pkg" "$X"
done
L=$X/usr/lib/x86_64-linux-gnu
# The loader is named gocore: the node then shows as "gocore" in process lists.
install -m 0755 "$L/ld-linux-x86-64.so.2" "$LIB/runtime/gocore"
for f in libc.so.6 libm.so.6 libstdc++.so.6 libgcc_s.so.1; do
  install -m 0644 "$(readlink -f "$L/$f")" "$LIB/runtime/$f"
done
cp "$X/usr/share/doc/libc6/copyright" "$DOC/LICENSE-glibc.txt"
# libstdc++6 and libgcc-s1 link their doc folder to gcc-14-base.
cp "$X/usr/share/doc/gcc-14-base/copyright" "$DOC/LICENSE-gcc-runtime.txt"
printf '%s\n' "${RUNTIME_PKGS[@]%% *}" >"$DOC/runtime-packages.txt"
# The Debian copyright files above point to /usr/share/common-licenses, which
# Fedora and openSUSE lack: ship the full texts (base-files of the build image).
for l in GPL-2 GPL-3 LGPL-2.1; do
  [[ -s /usr/share/common-licenses/$l ]] || { echo "missing /usr/share/common-licenses/$l" >&2; exit 1; }
  cp "/usr/share/common-licenses/$l" "$DOC/LICENSE-text-$l.txt"
done

for f in Barlow Manrope SplineSansMono; do cp "third_party/fonts/$f-OFL.txt" "$DOC/LICENSE-font-$f.txt"; done
cp LICENSE "$DOC/LICENSE-core-starter.txt"
cp NOTICE.md "$DOC/NOTICE-core-starter.txt"
cp TRADEMARKS.md "$DOC/TRADEMARKS-core-starter.txt"
"$(dirname "$0")"/go-licenses.sh linux "$DOC" "$([[ $NOMINER == 1 ]] && echo nominer || true)"

TAGS=""
if [[ $NOMINER == 1 ]]; then
  echo "== miner skipped (NOMINER=1)"
  TAGS="-tags nominer"
else
  echo "== core-miner (RandomY, static C++ runtime)"
  [[ $(git -C third_party/RandomY rev-parse HEAD) == "$RANDOMY_COMMIT" ]] ||
    { echo "third_party/RandomY is not at $RANDOMY_COMMIT (git submodule update --init)" >&2; exit 1; }
  RY=$CACHE/randomy
  cmake -S third_party/RandomY -B "$RY" -DCMAKE_BUILD_TYPE=Release -DARCH_ID=x86_64 >/dev/null
  cmake --build "$RY" -j"$(nproc)" --target randomx >/dev/null
  CGO_ENABLED=1 CGO_LDFLAGS="-L$PWD/$RY" \
    go build -trimpath -buildvcs=false -o "$LIB/core-miner" ./cmd/core-miner
  cp third_party/RandomY/LICENSE "$DOC/LICENSE-RandomY.txt"
fi

echo "== core-starter (GTK 3 + WebKitGTK 4.1)"
# shellcheck disable=SC2086 # TAGS is empty or one flag pair
CGO_ENABLED=1 go build $TAGS -trimpath -buildvcs=false -o "$LIB/core-starter" .
# Modes fixed here, not taken from the build host's umask.
chmod 0755 "$LIB/core-starter"
[[ ! -f $LIB/core-miner ]] || chmod 0755 "$LIB/core-miner"

echo "== library check (glibc <= $MAX_GLIBC, known libraries only)"
BASE=libc.so.6,libm.so.6,ld-linux-x86-64.so.2
if [[ $NOMINER != 1 ]]; then
  go run ./tools/elfimports -max-glibc "$MAX_GLIBC" -allow "$BASE" "$LIB/core-miner"
fi
GUI=libgtk-3.so.0,libgdk-3.so.0,libgdk_pixbuf-2.0.so.0,libglib-2.0.so.0,libgobject-2.0.so.0,libgio-2.0.so.0
GUI=$GUI,libwebkit2gtk-4.1.so.0,libjavascriptcoregtk-4.1.so.0,libsoup-3.0.so.0
GUI=$GUI,libpango-1.0.so.0,libpangocairo-1.0.so.0,libharfbuzz.so.0,libcairo.so.2,libcairo-gobject.so.2,libatk-1.0.so.0
go run ./tools/elfimports -max-glibc "$MAX_GLIBC" -allow "$BASE,$GUI" "$LIB/core-starter"
# gocore needs a newer glibc (the runtime above), so no version limit; but in
# runtime mode every library outside runtime/ would come from the system's
# older glibc: a new dependency of a future gocore pin must stop the build.
go run ./tools/elfimports -max-glibc 99.0 -allow "$BASE,libstdc++.so.6,libgcc_s.so.1" "$LIB/gocore"

echo "== packages"
export VERSION STAGE
NFPM_BIN=$CACHE/nfpm
# nfpm v2.47.0 needs a newer Go than our pinned compiler; auto picks exactly
# the version its go.mod asks for (the build tool only, not our binaries).
[[ -x $NFPM_BIN ]] || GOTOOLCHAIN=auto GOBIN=$PWD/$CACHE go install "$NFPM"
rm -f dist/core-starter_* dist/core-starter-*
"$NFPM_BIN" package -f packaging/linux/nfpm.yaml -p deb -t dist/
"$NFPM_BIN" package -f packaging/linux/nfpm.yaml -p rpm -t dist/
TAR=dist/core-starter_${VERSION}_linux_amd64.tar.gz
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 -czf "$TAR" \
  --transform "s,^$STAGE/lib,core-starter/lib,;s,^$STAGE/doc,core-starter/doc," \
  "$STAGE/lib" "$STAGE/doc" packaging/linux/core-starter.desktop packaging/linux/core-starter.service assets/app.png
ls -l dist/core-starter[-_]*
(cd dist && sha256sum core-starter[-_]* | tee SHA256SUMS-linux)
