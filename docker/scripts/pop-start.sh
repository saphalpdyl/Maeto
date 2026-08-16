#!/bin/sh
# PoP entrypoint: charon alongside FRR.
#
# charon is strongSwan's IKE daemon. maeto-agent drives it over the vici unix
# socket (/var/run/charon.vici) rather than by templating swanctl.conf, so it
# has to be listening before any tunnel intent arrives. FRR stays in the
# foreground so the node's liveness looks the same to containerlab as before,
# and tini (the inherited entrypoint) reaps charon.
set -e

mkdir -p /var/run

/usr/lib/strongswan/charon &

exec /usr/lib/frr/docker-start
