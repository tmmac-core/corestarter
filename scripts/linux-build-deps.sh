#!/usr/bin/env bash
# Installs what scripts/build-linux.sh needs on Ubuntu/Debian (CI: ubuntu:22.04).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq --no-install-recommends \
  ca-certificates curl git build-essential cmake pkg-config \
  libgtk-3-dev libwebkit2gtk-4.1-dev >/dev/null
