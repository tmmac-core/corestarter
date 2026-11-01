#!/usr/bin/env bash
# Builds dist/CoreStarter.exe on Linux (CI, the release build) or macOS.
#
# 1. Downloads the official gocore.exe release and the MinGW runtime DLLs it
#    links against (libstdc++-6.dll is not part of the release), verifies every
#    file against the SHA-256 pinned below.
# 2. Builds core-miner.exe: RandomY (submodule third_party/RandomY, pinned
#    commit) with MinGW-w64, statically linked, plus the Go miner via cgo.
#    NOMINER=1 skips this and builds the app without mining (e.g. on a Mac
#    without MinGW); MINGW_PREFIX picks the cross compiler.
# 3. Gzips everything into payload/ and generates payload_gen_windows.go.
# 4. Embeds icon + manifest (go-winres) and builds the Windows GUI executable.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.25.14 # same compiler on every build host
export LC_ALL=C # same file order (globs, sort) on macOS and Linux

GOCORE_VERSION=v2.2.2
GOCORE_URL=https://github.com/core-coin/go-core/releases/download/$GOCORE_VERSION/gocore-windows-x86_64.exe
GOCORE_SHA=a0e1f0bea509bc4b6df3c6468b698ad0e74a4113344efb0b238ca3a394a9c831
GOCORE_LICENSE_URL=https://raw.githubusercontent.com/core-coin/go-core/$GOCORE_VERSION/LICENSE
GOCORE_LICENSE_SHA=e2cbe1260d2307e00c04cdc94c85c9e254dde6a17281799a92b8a557d8db1812

# MSYS2 UCRT64 packages: same ABI as the toolchain the release was linked with
# (posix threads, SEH, UCRT). Package hashes from the ucrt64.db index, 2026-10-17.
MSYS2=https://repo.msys2.org/mingw/ucrt64
PKGS=(
  "mingw-w64-ucrt-x86_64-libstdc++-16.2.0-6-any.pkg.tar.zst 33c5e58a85a538cf843a8672991cce591f0231d66da3b66a57629712d64be17f"
  "mingw-w64-ucrt-x86_64-libgcc-16.2.0-6-any.pkg.tar.zst 80b2497f019977dd9528b1abc8621b1328a0115ab1f724047ccbb56bf323f11d"
  "mingw-w64-ucrt-x86_64-libwinpthread-14.0.0.r426.g4564ee4b5-2-any.pkg.tar.zst a02f5e09d666d3be657c78c86fe67c975d04fe33b5211cd5ff1b23a91c118524"
)
DLLS=(
  "libstdc++-6.dll 4e4822dc4aaa99383f581dae7ab9464ae89346c5ca742f7efbcdebe20b0fe7b5"
  "libgcc_s_seh-1.dll 791ebf66e99c7c1b4ed3666468cec45a0172b4c8942ac6c84c3e9cab3e1b5270"
  "libwinpthread-1.dll e765ee6109a0080a7c42848911f4349099b3c15aa734d845cb0e438f6f5a91b5"
)
GO_WINRES=github.com/tc-hib/go-winres@v0.3.3
RANDOMY_COMMIT=c9d185a055b7604e8d58059031e0e33dfe577cc5 # core-coin/RandomY v1.1.17
NOMINER=${NOMINER:-0}
MINGW_PREFIX=${MINGW_PREFIX:-x86_64-w64-mingw32}

CACHE=.cache
STAGE=$CACHE/stage
# Start from an empty stage: everything in it gets embedded.
rm -rf "$STAGE"
mkdir -p "$CACHE" "$STAGE" payload dist

sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
fetch() { # url dest sha
  if [[ ! -f $2 || $(sha "$2") != "$3" ]]; then
    # Download servers answer 503 now and then (Launchpad, 27.10.).
    curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 -o "$2.part" "$1"
    mv "$2.part" "$2"
  fi
  [[ $(sha "$2") == "$3" ]] || { echo "SHA-256 mismatch: $2" >&2; exit 1; }
}

echo "== gocore $GOCORE_VERSION"
fetch "$GOCORE_URL" "$CACHE/gocore-$GOCORE_VERSION.exe" "$GOCORE_SHA"
cp "$CACHE/gocore-$GOCORE_VERSION.exe" "$STAGE/gocore.exe"
fetch "$GOCORE_LICENSE_URL" "$CACHE/LICENSE-gocore-$GOCORE_VERSION.txt" "$GOCORE_LICENSE_SHA"
cp "$CACHE/LICENSE-gocore-$GOCORE_VERSION.txt" "$STAGE/LICENSE-gocore.txt"
# go-core's file headers say LGPL-3.0-or-later (library) and GPL-3.0-or-later (the
# command, whose text is LICENSE-core-starter.txt): their texts go along too.
cp third_party/licenses/LGPL-3.0.txt "$STAGE/LICENSE-gocore-LGPL3.txt"

echo "== MinGW runtime DLLs"
rm -rf "$CACHE/x" && mkdir -p "$CACHE/x"
for entry in "${PKGS[@]}"; do
  read -r pkg want <<<"$entry"
  fetch "$MSYS2/$pkg" "$CACHE/$pkg" "$want"
  zstd -dc "$CACHE/$pkg" | tar -x -C "$CACHE/x"
done
for entry in "${DLLS[@]}"; do
  read -r dll want <<<"$entry"
  cp "$CACHE/x/ucrt64/bin/$dll" "$STAGE/$dll"
  [[ $(sha "$STAGE/$dll") == "$want" ]] || { echo "SHA-256 mismatch: $dll" >&2; exit 1; }
done
cp "$CACHE/x/ucrt64/share/licenses/libstdc++/COPYING.RUNTIME" "$STAGE/LICENSE-gcc-runtime-exception.txt"
cp "$CACHE/x/ucrt64/share/licenses/libstdc++/COPYING3" "$STAGE/LICENSE-gcc-GPL3.txt"
cp "$CACHE/x/ucrt64/share/licenses/libwinpthread/COPYING" "$STAGE/LICENSE-winpthreads.txt"

# The window's fonts (embedded in the exe, ui/fonts): their OFL texts go with the other licences.
for f in Barlow Manrope SplineSansMono; do cp "third_party/fonts/$f-OFL.txt" "$STAGE/LICENSE-font-$f.txt"; done
# CoreStarter's own licence (GPL-3.0 + section 7 terms) next to them.
cp LICENSE "$STAGE/LICENSE-core-starter.txt"
cp NOTICE.md "$STAGE/NOTICE-core-starter.txt"
cp TRADEMARKS.md "$STAGE/TRADEMARKS-core-starter.txt"
"$(dirname "$0")"/go-licenses.sh windows "$STAGE" "$([[ $NOMINER == 1 ]] && echo nominer || true)"
GOLIC="LICENSE-go.txt LICENSE-go-go-ole.txt LICENSE-go-go-webview2.txt LICENSE-go-go-winio.txt LICENSE-go-systray.txt LICENSE-go-webviewloader.txt LICENSE-go-x-sys.txt"
EXPECTED="$GOLIC LICENSE-core-starter.txt LICENSE-font-Barlow.txt LICENSE-font-Manrope.txt LICENSE-font-SplineSansMono.txt LICENSE-gcc-GPL3.txt LICENSE-gcc-runtime-exception.txt LICENSE-gocore-LGPL3.txt LICENSE-gocore.txt LICENSE-winpthreads.txt NOTICE-core-starter.txt TRADEMARKS-core-starter.txt gocore.exe libgcc_s_seh-1.dll libstdc++-6.dll libwinpthread-1.dll"
EXPECTED=$(printf '%s\n' $EXPECTED | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')
TAGS=""
if [[ $NOMINER == 1 ]]; then
  echo "== miner skipped (NOMINER=1)"
  TAGS="-tags nominer"
else
  echo "== core-miner.exe (RandomY + MinGW, static)"
  CC_W="$MINGW_PREFIX-gcc-posix" CXX_W="$MINGW_PREFIX-g++-posix"
  command -v "$CC_W" >/dev/null || { echo "$CC_W not found: install mingw-w64 or build with NOMINER=1" >&2; exit 1; }
  [[ $(git -C third_party/RandomY rev-parse HEAD) == "$RANDOMY_COMMIT" ]] || { echo "third_party/RandomY is not at $RANDOMY_COMMIT (git submodule update --init)" >&2; exit 1; }
  RY_BUILD=$CACHE/randomy-win
  cmake -S third_party/RandomY -B "$RY_BUILD" -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_SYSTEM_NAME=Windows -DCMAKE_SYSTEM_PROCESSOR=x86_64 -DARCH_ID=x86_64 \
    -DCMAKE_C_COMPILER="$CC_W" -DCMAKE_CXX_COMPILER="$CXX_W" >/dev/null
  cmake --build "$RY_BUILD" -j4 --target randomx >/dev/null
  CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="$CC_W" CXX="$CXX_W" CGO_LDFLAGS="-L$PWD/$RY_BUILD" \
    go build -trimpath -buildvcs=false -o "$STAGE/core-miner.exe" ./cmd/core-miner
  cp third_party/RandomY/LICENSE "$STAGE/LICENSE-RandomY.txt"
  # The static miner carries parts of the MinGW-w64 runtime and winpthreads of the
  # build system's cross compiler: their notices from its Debian/Ubuntu package.
  MW=""
  for c in /usr/share/doc/mingw-w64-x86-64-dev/copyright /usr/share/doc/mingw-w64-common/copyright; do
    [[ -s $c ]] && { MW=$c; break; }
  done
  [[ -n $MW ]] || { echo "copyright of the mingw-w64 package not found" >&2; exit 1; }
  cp "$MW" "$STAGE/LICENSE-mingw-w64-runtime.txt"
  # shellcheck disable=SC2086 # word splitting is the point
  EXPECTED=$(printf '%s\n' $EXPECTED LICENSE-RandomY.txt LICENSE-mingw-w64-runtime.txt core-miner.exe | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')
fi

echo "== DLL imports"
# core-miner.exe must only need Windows system DLLs (static C++ runtime).
go run ./tools/peimports "$STAGE" "$STAGE"/*.exe

ACTUAL=$(cd "$STAGE" && ls | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')
[[ "$ACTUAL" == "$EXPECTED" ]] || { echo "unexpected stage content: $ACTUAL" >&2; exit 1; }

echo "== payload"
rm -f payload/*.gz
gen=payload_gen_windows.go
{
  echo "// Code generated by scripts/build.sh. DO NOT EDIT."
  echo
  echo "//go:build windows"
  echo
  echo "package main"
  echo
  echo "const gocoreVersion = \"$GOCORE_VERSION\""
  echo
  echo "var payloadFiles = []payloadFile{"
  for f in "$STAGE"/*; do
    name=$(basename "$f")
    python3 -I -c 'import gzip,sys; sys.stdout.buffer.write(gzip.compress(open(sys.argv[1],"rb").read(), 9, mtime=0))' "$f" >"payload/$name.gz"
    echo "	{Name: \"$name\", SHA256: \"$(sha "$f")\"},"
  done
  echo "}"
} >"$gen"
gofmt -w "$gen"

echo "== resources"
VERSION=$(sed -n 's/^\tappVersion *= *"\(.*\)"/\1/p' app.go)
[[ -n $VERSION ]] || { echo "appVersion not found in app.go" >&2; exit 1; }
rm -f rsrc_windows_*.syso
GOOS= GOARCH= go run "$GO_WINRES" simply \
  --arch amd64 \
  --icon assets/tray.ico \
  --manifest gui \
  --product-name "CoreStarter" \
  --file-description "CoreStarter" \
  --product-version "$VERSION" --file-version "$VERSION" \
  --copyright "TMMAC" \
  --original-filename CoreStarter.exe

echo "== build"
# shellcheck disable=SC2086 # TAGS is empty or one flag pair
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $TAGS -trimpath -buildvcs=false -ldflags "-H windowsgui" -o dist/CoreStarter.exe .
go run ./tools/peimports -known dist/CoreStarter.exe
ls -l dist/CoreStarter.exe
sha dist/CoreStarter.exe | tee dist/CoreStarter.exe.sha256
