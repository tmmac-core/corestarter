#!/usr/bin/env bash
# Builds the macOS app: dist/CoreStarter.app and dist/CoreStarter-<version>-mac-arm64.dmg
# (Apple silicon, macOS 15+). MAC_ARCH=x86_64 builds the Intel version instead:
# dist/intel/CoreStarter.app and dist/CoreStarter-<version>-mac-intel.dmg.
# Runs on a Mac with Apple silicon, the Command Line Tools, Go (go.mod's
# version) and cmake (the Intel version is cross-compiled).
#
# 1. Downloads the official gocore release for the architecture and its
#    licence, verifies both against the SHA-256 pinned below. gocore goes into
#    the app bit for bit: arm64 with its own (linker) signature, x86_64 as
#    released, unsigned (neither Go's nor Apple's linker signs Intel code).
# 2. Builds core-miner (RandomY, pinned submodule, generic armv8-a+crypto or
#    x86-64 with AES, so it runs on every chip of the kind) and the starter
#    (cgo: AppKit + WKWebView).
# 3. Puts together CoreStarter.app with the menu bar helper inside, signs it
#    ad hoc (no Developer ID yet), checks it and
#    packs the DMG.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.25.14 # same compiler on every build host
export LC_ALL=C
# Our binaries run from macOS 15 on, like the official gocore (minos 15.0).
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_CFLAGS="-O2 -mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"
export CGO_LDFLAGS="-mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"

MAC_ARCH=${MAC_ARCH:-arm64}
case $MAC_ARCH in
arm64)
  GOCORE_VERSION=v2.2.2
  GOCORE_SHA=9b16d813afd1b976059d30541a1f7c4527ddd55e616454c6c90aed56c3baf87a
  export GOARCH=arm64 ARCH_NAME=arm64
  ;;
x86_64)
  # Core dropped darwin-x86_64 from its release builds with v2.2.2 (go-core
  # commit "remove macos amd64 x86_64 support from GA"). v2.2.1 is the last
  # official Intel file; v2.2.2 only adds an RPC parameter (balanceOf), no
  # consensus change. Raising the arm64 version does not touch this pin:
  # check each new release for darwin-x86_64 and for consensus changes, and
  # stop shipping the Intel build if the network moves past v2.2.1.
  GOCORE_VERSION=v2.2.1
  GOCORE_SHA=2368f0b48ae76f6cbc63b16bb8b9deead249bfd9418678f209eb08b74f13dd3c
  export GOARCH=amd64 ARCH_NAME=intel
  ;;
*) echo "MAC_ARCH is arm64 or x86_64" >&2; exit 1 ;;
esac
GOCORE_URL=https://github.com/core-coin/go-core/releases/download/$GOCORE_VERSION/gocore-darwin-$MAC_ARCH
GOCORE_LICENSE_URL=https://raw.githubusercontent.com/core-coin/go-core/$GOCORE_VERSION/LICENSE
GOCORE_LICENSE_SHA=e2cbe1260d2307e00c04cdc94c85c9e254dde6a17281799a92b8a557d8db1812
RANDOMY_COMMIT=c9d185a055b7604e8d58059031e0e33dfe577cc5 # core-coin/RandomY v1.1.17
NOMINER=${NOMINER:-0}
# E2E=1: test build for this Mac only (offline node, miner on tools/fakepool;
# e2e_darwin.go). Never distribute it.
E2E=${E2E:-0}

[[ $(uname -s) == Darwin && $(uname -m) == arm64 ]] || { echo "build on a Mac with Apple silicon" >&2; exit 1; }
grep -q "const gocoreVersion = \"$GOCORE_VERSION\"" "gocore_darwin_$GOARCH.go" ||
  { echo "gocore_darwin_$GOARCH.go does not say $GOCORE_VERSION" >&2; exit 1; }
VERSION=$(sed -n 's/^\tappVersion *= *"\(.*\)"/\1/p' app.go)
[[ -n $VERSION ]] || { echo "appVersion not found in app.go" >&2; exit 1; }

CACHE=.cache/mac
APP="dist/CoreStarter.app"
[[ $MAC_ARCH == arm64 ]] || APP="dist/intel/CoreStarter.app"
HELPER="$APP/Contents/Library/LoginItems/CoreStarter Helper.app"
MACOS=$APP/Contents/MacOS
DOC=$APP/Contents/Resources/licenses
rm -rf "$APP"
mkdir -p "$CACHE" "$MACOS" "$DOC" "$HELPER/Contents/MacOS" "$HELPER/Contents/Resources"

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
fetch "$GOCORE_URL" "$CACHE/gocore-$GOCORE_VERSION-$MAC_ARCH" "$GOCORE_SHA"
install -m 0755 "$CACHE/gocore-$GOCORE_VERSION-$MAC_ARCH" "$MACOS/gocore"
fetch "$GOCORE_LICENSE_URL" "$CACHE/LICENSE-gocore-$GOCORE_VERSION.txt" "$GOCORE_LICENSE_SHA"
cp "$CACHE/LICENSE-gocore-$GOCORE_VERSION.txt" "$DOC/LICENSE-gocore.txt"
cp third_party/licenses/LGPL-3.0.txt "$DOC/LICENSE-gocore-LGPL3.txt" # go-core's file headers (NOTICE.md)
for f in Barlow Manrope SplineSansMono; do cp "third_party/fonts/$f-OFL.txt" "$DOC/LICENSE-font-$f.txt"; done
cp LICENSE "$DOC/LICENSE-core-starter.txt"
cp NOTICE.md "$DOC/NOTICE-core-starter.txt"
cp TRADEMARKS.md "$DOC/TRADEMARKS-core-starter.txt"
"$(dirname "$0")"/go-licenses.sh darwin "$DOC" "$([[ $NOMINER == 1 ]] && echo nominer || true)"

TAGS=""
if [[ $NOMINER == 1 ]]; then
  echo "== miner skipped (NOMINER=1)"
  TAGS="-tags nominer"
else
  echo "== core-miner (RandomY, $MAC_ARCH)"
  [[ $(git -C third_party/RandomY rev-parse HEAD) == "$RANDOMY_COMMIT" ]] ||
    { echo "third_party/RandomY is not at $RANDOMY_COMMIT (git submodule update --init)" >&2; exit 1; }
  RY=$CACHE/randomy-$MAC_ARCH
  # RandomY's CMakeLists sets the deployment target 10.12 itself; the
  # included file puts ours back (see there). It picks its sources (JIT,
  # assembler) by ARCH_ID, which would be this Mac's arm64 without the flag.
  cmake -S third_party/RandomY -B "$RY" -DCMAKE_BUILD_TYPE=Release -DARCH=default \
    -DARCH_ID="$MAC_ARCH" -DCMAKE_OSX_ARCHITECTURES="$MAC_ARCH" \
    -DCMAKE_PROJECT_INCLUDE="$PWD/scripts/mac/randomy-target.cmake" >/dev/null
  cmake --build "$RY" -j"$(sysctl -n hw.ncpu)" --target randomx >/dev/null
  CGO_ENABLED=1 CGO_LDFLAGS="$CGO_LDFLAGS -L$PWD/$RY" \
    go build -trimpath -buildvcs=false -o "$MACOS/core-miner" ./cmd/core-miner
  cp third_party/RandomY/LICENSE "$DOC/LICENSE-RandomY.txt"
fi

if [[ $E2E == 1 ]]; then
  echo "== E2E TEST BUILD: offline node, miner on 127.0.0.1:47999"
  TAGS=${TAGS:+$TAGS,e2e}
  TAGS=${TAGS:-"-tags e2e"}
fi

echo "== core-starter (AppKit + WKWebView)"
# shellcheck disable=SC2086 # TAGS is empty or one flag pair
CGO_ENABLED=1 go build $TAGS -trimpath -buildvcs=false -ldflags=-w -o "$MACOS/core-starter" .
# The helper is its own app for macOS (menu bar, no Dock icon): a copy, not a
# link, so each bundle carries its own signature.
cp "$MACOS/core-starter" "$HELPER/Contents/MacOS/core-starter"
cp assets/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"
cp assets/AppIcon.icns "$HELPER/Contents/Resources/AppIcon.icns"

# plist <path> <bundle id> <name> <extra keys>
plist() {
  cat >"$1" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>$2</string>
	<key>CFBundleName</key><string>$3</string>
	<key>CFBundleDisplayName</key><string>$3</string>
	<key>CFBundleExecutable</key><string>core-starter</string>
	<key>CFBundleIconFile</key><string>AppIcon</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>$VERSION</string>
	<key>CFBundleVersion</key><string>$VERSION</string>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>CFBundleDevelopmentRegion</key><string>en</string>
	<key>LSMinimumSystemVersion</key><string>$MACOSX_DEPLOYMENT_TARGET</string>
	<key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSHumanReadableCopyright</key><string>TMMAC (tellmemoreaboutcore.com)</string>
	<key>NSLocalNetworkUsageDescription</key><string>The Core node asks your router (UPnP) to open a port so other nodes can reach it. Declining is fine: the node still syncs, with fewer connections.</string>
$4</dict>
</plist>
EOF
}
# The reason macOS shows in its "Local Network" dialog, in the user's language
# (the dialog comes up when gocore asks the router via UPnP).
localnet() { # lang text
  local b
  for b in "$APP" "$HELPER"; do
    mkdir -p "$b/Contents/Resources/$1.lproj"
    printf '"NSLocalNetworkUsageDescription" = "%s";\n' "$2" >"$b/Contents/Resources/$1.lproj/InfoPlist.strings"
  done
}
localnet en "The Core node asks your router (UPnP) to open a port so other nodes can reach it. Declining is fine: the node still syncs, with fewer connections."
localnet de "Die Core-Node bittet deinen Router (UPnP), einen Port zu öffnen, damit andere Nodes sie erreichen. Ablehnen ist in Ordnung: Die Node synchronisiert trotzdem, nur mit weniger Verbindungen."
localnet ja "Coreノードは、他のノードから接続できるように、ルーター（UPnP）にポートを開くよう求めます。許可しなくても問題ありません。接続数は減りますが、同期は続きます。"
localnet nl "De Core-node vraagt je router (UPnP) een poort te openen, zodat andere nodes hem kunnen bereiken. Weigeren mag: de node synchroniseert dan nog steeds, met minder verbindingen."
localnet ro "Nodul Core cere routerului tău (UPnP) să deschidă un port, ca alte noduri să-l poată accesa. Poți refuza: nodul se sincronizează în continuare, cu mai puține conexiuni."
plist "$APP/Contents/Info.plist" info.tmmac.core-starter "CoreStarter" ""
plist "$HELPER/Contents/Info.plist" info.tmmac.core-starter.helper "CoreStarter" \
  "	<key>LSUIElement</key><true/>
"
chmod -R u=rwX,go=rX "$APP"

echo "== library check (system libraries only, macOS <= $MACOSX_DEPLOYMENT_TARGET)"
check_macho() { # file
  local bad archs
  archs=$(lipo -archs "$1")
  [[ $archs == "$MAC_ARCH" ]] || { echo "$1 is $archs, not $MAC_ARCH" >&2; exit 1; }
  bad=$(otool -L "$1" | tail -n +2 | awk '{print $1}' | grep -v -E '^(/usr/lib/|/System/Library/)' || true)
  [[ -z $bad ]] || { echo "$1 links outside the system: $bad" >&2; exit 1; }
  local minos
  minos=$(otool -l "$1" | awk '/LC_BUILD_VERSION/{f=1} f&&/minos/{print $2; exit}')
  [[ $(printf '%s\n%s\n' "$minos" "$MACOSX_DEPLOYMENT_TARGET" | sort -V | tail -1) == "$MACOSX_DEPLOYMENT_TARGET" ]] ||
    { echo "$1 needs macOS $minos" >&2; exit 1; }
  echo "  $(basename "$1"): $archs, minos $minos, system libraries only"
}
for f in "$MACOS"/*; do check_macho "$f"; done

echo "== sign (ad hoc, inside out)"
GOCORE_SHIP=$GOCORE_SHA # what the bundle's gocore must hash to at the end
if [[ $MAC_ARCH == x86_64 ]]; then
  # The Intel gocore comes unsigned, and codesign refuses a bundle with
  # unsigned code inside. Sign it ad hoc, then prove the pin: without the
  # signature again it must be the release byte for byte, except the vmsize
  # of __LINKEDIT (tools/machopin). The ad hoc signature is deterministic, so
  # a fresh signature of the pinned file is what may ship, signature included.
  ORIG=$CACHE/gocore-$GOCORE_VERSION-$MAC_ARCH
  [[ $(sha "$ORIG") == "$GOCORE_SHA" ]] || { echo "SHA-256 mismatch: $ORIG" >&2; exit 1; }
  PIN=$(mktemp -d)
  cp "$ORIG" "$PIN/gocore"
  codesign -s - "$PIN/gocore"
  GOCORE_SHIP=$(sha "$PIN/gocore")
  cp "$PIN/gocore" "$PIN/unsigned"
  codesign --remove-signature "$PIN/unsigned"
  # machopin runs on this Mac, not as the Intel build target.
  env -u GOARCH go run ./tools/machopin "$ORIG" "$PIN/unsigned"
  rm -rf "$PIN"
  codesign -f -s - "$MACOS/gocore"
fi
[[ ! -f $MACOS/core-miner ]] || codesign -f -s - "$MACOS/core-miner"
codesign -f -s - "$HELPER"
codesign -f -s - "$APP"
codesign --verify --deep --strict "$APP"
[[ $(sha "$MACOS/gocore") == "$GOCORE_SHIP" ]] || { echo "gocore changed while packing" >&2; exit 1; }

echo "== dmg"
DMG=dist/CoreStarter-$VERSION-mac-$ARCH_NAME.dmg
[[ $E2E != 1 ]] || DMG=dist/CoreStarter-$VERSION-mac-$ARCH_NAME-e2e.dmg
STAGE=$CACHE/dmg-$MAC_ARCH
rm -rf "$STAGE" "$DMG"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Programme · Applications"
cp packaging/mac/guide.html "$STAGE/Anleitung · Guide.html" # Gatekeeper steps for testers (unsigned build)
hdiutil create -quiet -volname "CoreStarter" -srcfolder "$STAGE" -fs APFS -format UDZO "$DMG"
ls -l "$DMG"
SUMS=SHA256SUMS-mac
[[ $MAC_ARCH == arm64 ]] || SUMS=SHA256SUMS-mac-intel
(cd dist && shasum -a 256 "$(basename "$DMG")" | tee "$SUMS")
