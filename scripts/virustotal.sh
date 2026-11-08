#!/usr/bin/env bash
# Checks release files on VirusTotal through its API v3 and prints the result per file.
#   scripts/virustotal.sh <file>...
# Asks VirusTotal for the file's SHA-256 first; only a file it does not know yet is
# uploaded (VirusTotal publishes it and its report, so run this after the GitHub release).
# Waits for the analysis and prints flagged/total with the names of the flagging engines,
# and for CoreStarter.exe the scan line of the download page (src/data/apps.js).
# A result with fewer than VT_MIN_TOTAL judging engines counts as unfinished (an analysis
# still running after an interrupted run, or one started by someone else) and stops.
# API key: $VT_API_KEY, else the macOS keychain item "virustotal-api" (docs/RELEASE.md).
# The key goes to curl through a header file, never as an argument (visible in ps).
set -euo pipefail
[[ $# -gt 0 ]] || { echo "usage: scripts/virustotal.sh <file>..." >&2; exit 1; }
command -v jq >/dev/null || { echo "jq not found" >&2; exit 1; }
ROOT=$(cd "$(dirname "$0")/.." && pwd) # file arguments stay relative to the caller

API=https://www.virustotal.com/api/v3
VERSION=$(sed -n 's/^\tappVersion *= *"\(.*\)"/\1/p' "$ROOT/app.go")
WAIT_MAX=${VT_WAIT_MAX:-1200} # seconds per file; a 30 MB exe usually takes a few minutes
PACE=${VT_PACE:-16}           # seconds between requests (tests set 0)
MIN_TOTAL=${VT_MIN_TOTAL:-50} # CoreStarter.exe got 69 to 71 judging engines

{ set +x; } 2>/dev/null # keep the key out of a bash -x trace
KEY=${VT_API_KEY:-}
unset VT_API_KEY # curl does not need it in its environment
if [[ -z $KEY ]] && command -v security >/dev/null; then
  KEY=$(security find-generic-password -s virustotal-api -w 2>/dev/null || true)
fi
[[ -n $KEY ]] || { echo "no API key: set VT_API_KEY or the keychain item virustotal-api" >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
umask 077
printf 'x-apikey: %s\n' "$KEY" >"$TMP/headers"
unset KEY

# The public API allows 4 requests a minute; one every 16 s stays below that.
last=0
# Not called in $(...): the subshell would lose $last and with it the pacing.
vt() { # vt <curl args>... : JSON body in $TMP/body, HTTP status in $TMP/status
  local now
  now=$(date +%s)
  if ((now - last < PACE)); then sleep $((PACE - (now - last))); fi
  last=$(date +%s)
  curl -sS -H @"$TMP/headers" -o "$TMP/body" -w '%{http_code}' "$@" >"$TMP/status" || echo 000 >"$TMP/status"
}
fail() { echo "$1: HTTP $(cat "$TMP/status") $(jq -r '.error.message? // empty' "$TMP/body" 2>/dev/null)" >&2; exit 1; }
field() { jq -er "$1" "$TMP/body" 2>/dev/null || { echo "$2: unexpected answer from VirusTotal" >&2; exit 1; }; }

# Prints "flagged|total|names|suspicious" from a file report or a finished analysis. Like the
# website, total counts the engines that judged the file (malicious, suspicious,
# undetected, harmless); engines that do not support the type or timed out are left out.
summary() {
  jq -r '(.data.attributes.last_analysis_results // .data.attributes.results // {}) as $r
    | [$r[] | select(.category == "malicious")] as $m
    | [$r[] | select(.category | IN("malicious", "suspicious", "undetected", "harmless"))] as $t
    | [$r[] | select(.category == "suspicious") | .engine_name] as $s
    | "\($m | length)|\($t | length)|\([$m[].engine_name] | sort | join(", "))|\($s | sort | join(", "))"' "$TMP/body"
}

for arg in "$@"; do
  [[ -f $arg ]] || { echo "$arg: not found" >&2; exit 1; }
  f=$(cd "$(dirname "$arg")" && pwd)/$(basename "$arg")
  sha=$(shasum -a 256 "$f" | awk '{print $1}')
  name=$(basename "$f")
  vt "$API/files/$sha"
  case $(cat "$TMP/status") in
  200) echo "== $name: known to VirusTotal, using its last analysis" ;;
  404)
    size=$(wc -c <"$f" | tr -d ' ')
    if ((size < 32000000)); then
      url=$API/files
    else # larger files go to a one-time upload URL (up to 650 MB)
      vt "$API/files/upload_url"
      [[ $(cat "$TMP/status") == 200 ]] || fail "$name upload_url"
      url=$(field .data "$name upload_url")
    fi
    echo "== $name: uploading $((size / 1048576)) MiB"
    vt -F "file=@$f" "$url"
    [[ $(cat "$TMP/status") == 200 ]] || fail "$name upload"
    id=$(field .data.id "$name upload")
    start=$(date +%s)
    while :; do
      vt "$API/analyses/$id"
      [[ $(cat "$TMP/status") == 200 ]] || fail "$name analysis"
      status=$(field .data.attributes.status "$name analysis")
      case $status in
      completed) break ;;
      queued | in-progress) ;;
      *) echo "$name: analysis status $status" >&2; exit 1 ;;
      esac
      (($(date +%s) - start < WAIT_MAX)) || { echo "$name: analysis not finished after ${WAIT_MAX}s, run again later" >&2; exit 1; }
    done
    ;;
  *) fail "$name lookup" ;;
  esac

  IFS="|" read -r flagged total names suspicious < <(summary) || true
  [[ ${total:-} =~ ^[0-9]+$ && ${flagged:-} =~ ^[0-9]+$ ]] || { echo "$name: no readable result" >&2; exit 1; }
  if ((total < MIN_TOTAL)); then
    echo "$name: only $total engines have judged it so far (analysis still running?), run again later" >&2
    echo "  https://www.virustotal.com/gui/file/$sha" >&2
    exit 1
  fi
  echo "$name: $flagged/$total${names:+ ($names)}${suspicious:+, suspicious: $suspicious}"
  echo "  https://www.virustotal.com/gui/file/$sha"
  if [[ $name == CoreStarter.exe ]]; then
    echo "  apps.js: scan: { version: \"$VERSION\", flagged: $flagged, total: $total${names:+, names: \"$names\"} },"
  fi
done
