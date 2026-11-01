#!/usr/bin/env bash
# Copies the licence texts of the Go code compiled into the programs (the Go
# standard library and every module the starter and the miner import for
# GOOS) into a folder: LICENSE-go.txt, LICENSE-go-<module>.txt.
# Usage: scripts/go-licenses.sh <goos> <dir> [build tags]
set -euo pipefail
cd "$(dirname "$0")/.."
GOOS_T=$1 OUT=$2 TAGS=${3:-}
cp "$(go env GOROOT)/LICENSE" "$OUT/LICENSE-go.txt"
pkgs=". ./cmd/core-miner"
[[ $TAGS == *nominer* ]] && pkgs="."
# -e: build.sh runs this before payload/ exists (fresh checkout), so the
# //go:embed pattern has no files yet; the import list is complete anyway.
# shellcheck disable=SC2086 # pkgs is a word list
GOOS=$GOOS_T CGO_ENABLED=1 go list -e -deps ${TAGS:+-tags "$TAGS"} \
  -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' $pkgs |
  sort -u | while read -r path dir; do
    name=$(basename "${path%/v[0-9]*}")
    [[ $path == golang.org/x/* ]] && name=x-$name
    lic=$(find "$dir" -maxdepth 1 -iname 'LICENSE*' -o -maxdepth 1 -iname 'COPYING*' | head -1)
    [[ -n $lic ]] || { echo "no licence file in $path" >&2; exit 1; }
    cp "$lic" "$OUT/LICENSE-go-$name.txt"
    # go-webview2's loader package (a Go port, ISC; Microsoft's WebView2Loader.dll
    # is only embedded with the build tag native_webview2loader, which we never set).
    if [[ $path == github.com/wailsapp/go-webview2 ]]; then
      cp "$dir/webviewloader/LICENSE" "$OUT/LICENSE-go-webviewloader.txt"
    fi
  done
