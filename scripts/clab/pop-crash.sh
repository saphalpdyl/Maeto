#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $(basename "$0") <state-file> <pop-node> [loud|silent]" >&2
  exit 2
}

[ $# -ge 2 ] && [ $# -le 3 ] || usage

state_file=$1
node=$2
mode=${3:-loud}

case $mode in
  loud|silent) ;;
  *) usage ;;
esac

cd "$(dirname "$0")/../.."

out=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['output'])" "$state_file")
read -r lab ifaces < <(python3 scripts/clab/pop_links.py "$out/topology.data.json" "$node")

container="clab-$lab-$node"
park="park-$node"

if sudo ip netns list | grep -qw "$park"; then
  echo "$node is already parked; run pop-boot first" >&2
  exit 1
fi

pid=$(sudo docker inspect -f '{{.State.Pid}}' "$container")

sudo ip netns add "$park"
for iface in $ifaces; do
  sudo nsenter -t "$pid" -n ip link set "$iface" netns "$park"
  if [ "$mode" = silent ]; then
    sudo ip -n "$park" link set "$iface" up
  fi
done

sudo docker kill "$container" > /dev/null

echo "$node crashed ($mode); links parked in $park"
