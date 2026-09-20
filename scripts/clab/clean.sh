#!/usr/bin/env bash
# Tear down the lab. Best effort: a missing state file or an already-destroyed
# lab still leaves the container sweep to run.
set -uo pipefail

usage() {
  echo "usage: $(basename "$0") <topology-name> <state-file>" >&2
  exit 2
}

[ $# -eq 2 ] || usage

topology_name=$1
state_file=$2

cd "$(dirname "$0")/../.."

if [ -f "$state_file" ]; then
  out=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['output'])" "$state_file")
  topology="$out/topology.yml"

  if [ -f "$topology" ]; then
    sudo containerlab destroy -t "$topology"
  else
    echo "missing $topology; skipping containerlab destroy" >&2
  fi
else
  echo "no $state_file; skipping containerlab destroy" >&2
fi

leftover=$(docker ps -aq --filter "name=^clab-$topology_name-")
if [ -n "$leftover" ]; then
  # shellcheck disable=SC2086
  sudo docker rm -f $leftover
fi

exit 0
