#!/usr/bin/env bash
# Runs in ubuntu:24.04 from the repo root (runtime-pins.yml, scripts/ci-local.sh):
# are the runtime packages pinned in scripts/build-linux.sh still the current
# ones of noble, noble-updates and noble-security? Exit 1 if one is stale.
set -eu
apt-get update -qq
stale=0
for deb in $(sed -n 's/^  "\([^ ]*\.deb\) .*/\1/p' scripts/build-linux.sh); do
  name=${deb%%_*}; rest=${deb#*_}; pinned=${rest%_*}
  current=$(apt-cache policy "$name" | sed -n 's/^  Candidate: //p')
  if [ "$pinned" = "$current" ]; then
    echo "ok    $name $pinned"
  else
    echo "STALE $name pinned $pinned, Ubuntu 24.04 has $current"; stale=1
  fi
done
exit $stale
