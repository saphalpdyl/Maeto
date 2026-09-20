#!/usr/bin/env bash
# Render the containerlab + frr config from the topology dsl into build/<hash>.
set -euo pipefail

usage() {
  echo "usage: $(basename "$0") <python> <topology-yaml>" >&2
  exit 2
}

[ $# -eq 2 ] || usage

python_bin=$1
topology_yaml=$2

cd "$(dirname "$0")/../.."

[ -x "$python_bin" ] || { echo "python $python_bin not found; run 'make install-virtual-environments'" >&2; exit 1; }
[ -f "$topology_yaml" ] || { echo "topology $topology_yaml not found" >&2; exit 1; }

export PYTHONPATH=clab
exec "$python_bin" -m generator "$topology_yaml"
