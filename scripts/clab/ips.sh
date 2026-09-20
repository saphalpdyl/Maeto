#!/usr/bin/env bash
# Print the global ipv6 addresses of every interface on each lab node.
set -euo pipefail

usage() {
  echo "usage: $(basename "$0") <topology-name>" >&2
  exit 2
}

[ $# -eq 1 ] || usage

topology_name=$1

printf "%-24s %s\n" "Name" "Interfaces"

for container in $(docker ps --format '{{.Names}}' | grep "^clab-$topology_name-"); do
  printf "%-24s\n" "$container"
  docker exec "$container" sh -c \
    "ip -6 -o addr show scope global | \
     awk '\$2 != \"eth0\" {printf \"  %-20s %s\n\", \$2\":\", \$4}'" \
    2>/dev/null || echo "  <none>"
  echo
done
