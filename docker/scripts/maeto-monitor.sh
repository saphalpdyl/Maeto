#!/bin/sh

stamp_time() {
  while read -r line; do
    echo "[$(date -Is)] $line"
  done
}

monitor_nft() {
  nft list ruleset | stamp_time
  nft monitor | stamp_time
}

monitor_ip() {
  ip -6 rule | stamp_time
  ip -6 monitor route rule | stamp_time
}

if [ "$1" = "nft" ]; then
  monitor_nft
elif [ "$1" = "ip" ]; then
  monitor_ip
else
  echo "usage: maeto-monitor nft|ip" >&2
  exit 1
fi
