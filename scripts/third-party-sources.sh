#!/usr/bin/env bash
# Source of the third-party programs and libraries the release packages carry
# (GPL-3.0 / LGPL: the source goes out from the same place as the binaries).
# Downloads the exact source of the pinned builds, checks each SHA-256 and
# writes dist/third-party-sources-<version>.tar with SOURCES.txt.
#   All:     gocore v2.2.2 (go-core: LICENSE says CORE License v1.0, the file
#            headers GPL-3.0-or-later / LGPL-3.0-or-later), with every Go module
#            it builds from (go mod vendor), and RandomY at the pinned commit.
# The tar is uploaded to every GitHub release next to the programs.
#   Windows: MinGW-w64 GCC runtime (libstdc++, libgcc_s) and winpthreads, the
#            MSYS2 builds pinned in scripts/build.sh.
#   Linux:   glibc and the GCC 14 runtime, the Ubuntu 24.04 builds pinned in
#            scripts/build-linux.sh.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=$(sed -n 's/^\tappVersion *= *"\(.*\)"/\1/p' app.go)
[[ -n $VERSION ]] || { echo "appVersion not found in app.go" >&2; exit 1; }

LP=https://launchpad.net/ubuntu/+archive/primary/+files
MSYS=https://repo.msys2.org/mingw/sources
# url-base file sha256
SOURCES=(
  "$MSYS mingw-w64-gcc-16.2.0-6.src.tar.zst a901d8afb78438abcff9ed020cd9aa9431db76de1992171c2454c3c07ad289e8"
  "$MSYS mingw-w64-winpthreads-14.0.0.r426.g4564ee4b5-2.src.tar.zst d2d540bb20b227f5d920d8f509e4e0c413e8310d08a96347c1bdeb9af8844d72"
  "$LP glibc_2.39-0ubuntu8.9.dsc 1b73a89e0910eb3e5920cde639ac7eb606256d8ee809688ff9b5912b67002711"
  "$LP glibc_2.39.orig.tar.xz f77bd47cf8170c57365ae7bf86696c118adb3b120d3259c64c502d3dc1e2d926"
  "$LP glibc_2.39.orig.tar.xz.asc 2cce427ef7933c17379f5514e4f0ccf8dffae5bf8c72f0f7e0bf8b8401f34be5"
  "$LP glibc_2.39-0ubuntu8.9.debian.tar.xz d6114f2302896a3c8612453e1bf1d0e61603e3029ba4a7c246fbb1ab4c48d2b0"
  "$LP gcc-14_14.2.0-4ubuntu2~24.04.1.dsc 50950080874a6ec6780dd60c243e21d9cda9d736bb32bca98d16095d27cc01b5"
  "$LP gcc-14_14.2.0.orig.tar.gz 768c314c11eeab56ccebb91eb42ec4a41122fa94f0d83400126401942622197b"
  "$LP gcc-14_14.2.0-4ubuntu2~24.04.1.debian.tar.xz cfece214c2fb790ef5f3baffb9a53e40618e7ae12d053610b251e94d77d08ade"
)

GOCORE_VERSION=v2.2.2
GOCORE_SRC_URL=https://github.com/core-coin/go-core/archive/refs/tags/$GOCORE_VERSION.tar.gz
GOCORE_SRC_SHA=1c563558934dd0215a715bca336d191dc51ea3fbdfb43e99f8801b04479ef97c
RANDOMY_COMMIT=c9d185a055b7604e8d58059031e0e33dfe577cc5 # as in scripts/build*.sh
for f in scripts/build.sh scripts/build-linux.sh scripts/build-mac.sh; do
  grep -q "^GOCORE_VERSION=$GOCORE_VERSION\$" "$f" || { echo "$f does not pin gocore $GOCORE_VERSION" >&2; exit 1; }
done

CACHE=${CACHE:-$HOME/.cache/core-starter/sources}
OUT=$PWD/dist/third-party-sources-$VERSION
mkdir -p "$CACHE" dist
rm -rf "$OUT" && mkdir -p "$OUT"

sha() { shasum -a 256 "$1" 2>/dev/null | cut -d' ' -f1 || sha256sum "$1" | cut -d' ' -f1; }

for entry in "${SOURCES[@]}"; do
  read -r base file want <<<"$entry"
  if [[ ! -f $CACHE/$file || $(sha "$CACHE/$file") != "$want" ]]; then
    echo "== fetch $file"
    curl -fsSL -o "$CACHE/$file.part" "$base/${file//\~/%7E}"
    mv "$CACHE/$file.part" "$CACHE/$file"
  fi
  [[ $(sha "$CACHE/$file") == "$want" ]] || { echo "SHA-256 mismatch: $file" >&2; exit 1; }
  cp "$CACHE/$file" "$OUT/$file"
done

echo "== go-core $GOCORE_VERSION with its Go modules"
GC=go-core-$GOCORE_VERSION.tar.gz
if [[ ! -f $CACHE/$GC || $(sha "$CACHE/$GC") != "$GOCORE_SRC_SHA" ]]; then
  curl -fsSL -o "$CACHE/$GC.part" "$GOCORE_SRC_URL" && mv "$CACHE/$GC.part" "$CACHE/$GC"
fi
[[ $(sha "$CACHE/$GC") == "$GOCORE_SRC_SHA" ]] || { echo "SHA-256 mismatch: $GC" >&2; exit 1; }
cp "$CACHE/$GC" "$OUT/$GC"
W=$(mktemp -d)
trap 'chmod -R u+w "$W" 2>/dev/null; rm -rf "$W"' EXIT
tar -xzf "$CACHE/$GC" -C "$W"
GCDIR=$(ls -d "$W"/go-core-*)
# go.sum pins every module; vendor/ holds their source as gocore was built.
(cd "$GCDIR" && GOFLAGS=-mod=mod go mod vendor)
tar -C "$GCDIR" -czf "$OUT/go-core-$GOCORE_VERSION-vendor.tar.gz" vendor go.mod go.sum

echo "== RandomY $RANDOMY_COMMIT"
[[ $(git -C third_party/RandomY rev-parse HEAD) == "$RANDOMY_COMMIT" ]] ||
  { echo "third_party/RandomY is not at $RANDOMY_COMMIT" >&2; exit 1; }
git -C third_party/RandomY archive --format=tar.gz --prefix="RandomY-${RANDOMY_COMMIT:0:7}/" \
  -o "$OUT/RandomY-${RANDOMY_COMMIT:0:7}.tar.gz" "$RANDOMY_COMMIT"

{
  echo "Source of the third-party programs and libraries in CoreStarter $VERSION"
  echo
  echo "CoreStarter's own source: https://github.com/tmmac-core/corestarter (tag v$VERSION)."
  echo "This archive holds the unchanged source of the third-party parts the release"
  echo "packages carry:"
  echo
  echo "All platforms: gocore $GOCORE_VERSION, the official Core Blockchain node, unchanged"
  echo "  release binary. Its LICENSE file says CORE License v1.0, its file headers"
  echo "  GPL-3.0-or-later (cmd) and LGPL-3.0-or-later (library). Source: $GC (the"
  echo "  tag from $GOCORE_SRC_URL, SHA-256 $GOCORE_SRC_SHA) and"
  echo "  go-core-$GOCORE_VERSION-vendor.tar.gz (every Go module it builds from, from"
  echo "  its go.sum; unpack into the source tree)."
  echo "All platforms: RandomY (core-miner, BSD-3-Clause) at commit $RANDOMY_COMMIT"
  echo "  of github.com/core-coin/RandomY: RandomY-${RANDOMY_COMMIT:0:7}.tar.gz."
  echo
  echo "Windows (CoreStarter.exe): libstdc++-6.dll, libgcc_s_seh-1.dll (GCC 16.2.0,"
  echo "  GPL-3.0 with GCC Runtime Library Exception) and libwinpthread-1.dll (MIT-style),"
  echo "  MSYS2 packages mingw-w64-ucrt-x86_64-{libstdc++,libgcc} 16.2.0-6 and"
  echo "  libwinpthread 14.0.0.r426.g4564ee4b5-2; source: MSYS2 source packages."
  echo "Linux (.deb, .rpm, .tar.gz): glibc 2.39 (LGPL-2.1) and libstdc++ / libgcc_s"
  echo "  (GCC 14.2.0, GPL-3.0 with GCC Runtime Library Exception), Ubuntu 24.04 packages"
  echo "  libc6 2.39-0ubuntu8.9 and gcc-14 14.2.0-4ubuntu2~24.04.1; source: Ubuntu source"
  echo "  packages (unpack with dpkg-source -x <file>.dsc)."
  echo
  echo "Everything is used unchanged. SHA-256 of the distribution packages:"
  echo
  for entry in "${SOURCES[@]}"; do
    read -r base file want <<<"$entry"
    echo "$want  $file"
    echo "  from $base/$file"
  done
} >"$OUT/SOURCES.txt"

tar -C dist -cf "dist/third-party-sources-$VERSION.tar" "third-party-sources-$VERSION"
rm -rf "$OUT"
echo "== dist/third-party-sources-$VERSION.tar ($(du -h "dist/third-party-sources-$VERSION.tar" | cut -f1))"
