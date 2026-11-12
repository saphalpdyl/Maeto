#!/usr/bin/env bash
# Bring a crashed pop back: the same container started again in a fresh network
# namespace
set -euo pipefail

usage() {
  echo "usage: $(basename "$0") <state-file> <pop-node>" >&2
  exit 2
}

[ $# -eq 2 ] || usage

state_file=$1
node=$2

cd "$(dirname "$0")/../.."

out=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['output'])" "$state_file")
read -r lab ifaces < <(python3 scripts/clab/pop_links.py "$out/topology.data.json" "$node")

container="clab-$lab-$node"
park="park-$node"

if ! sudo ip netns list | grep -qw "$park"; then
  echo "$node is not parked; nothing to boot" >&2
  exit 1
fi

sudo docker start "$container" > /dev/null

# docker start keeps the container's filesystem, so charon's pid file and vici
# socket from before the crash are still there and charon refuses to start.
# also, supervisord needs a moment to open its
# socket, hence the retries.
sudo docker exec "$container" rm -f /var/run/charon.pid /var/run/charon.vici
for i in $(seq 25); do
  sudo docker exec "$container" supervisorctl -c /etc/supervisor/conf.d/supervisord.conf restart charon \
    > /dev/null 2>&1 && break
  sleep 0.2
done

pid=$(sudo docker inspect -f '{{.State.Pid}}' "$container")
for iface in $ifaces; do
  sudo ip -n "$park" link set "$iface" netns "$pid"
done
sudo ip netns del "$park"

sudo docker exec -d "$container" sh -c 'sh /etc/maeto/boot.sh > /var/log/maeto-boot.log 2>&1'

echo "$node booting..."
