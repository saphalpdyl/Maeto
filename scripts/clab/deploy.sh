#!/usr/bin/env bash
# Deploy the topology recorded in the state file (build/<hash>/topology.yml).
set -euo pipefail

usage() {
  echo "usage: $(basename "$0") <state-file>" >&2
  exit 2
}

[ $# -eq 1 ] || usage

state_file=$1

cd "$(dirname "$0")/../.."

[ -f "$state_file" ] || { echo "no $state_file; run 'make generate' first" >&2; exit 1; }

out=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['output'])" "$state_file")
topology="$out/topology.yml"

[ -f "$topology" ] || { echo "missing $topology; run 'make generate' first" >&2; exit 1; }

sudo modprobe vrf
sudo clab deploy -t "$topology" --reconfigure
sudo sh "$out/mgmt_routes.sh"
