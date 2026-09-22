#!/bin/sh
# Lets containers reach this server by its own LAN address.
#
# On Docker Desktop a container that connects to the Windows host's LAN IP
# (e.g. 192.168.1.28:3500) is never delivered back to the host, so anything
# in the stack that uses the public address (pipeline clone steps, the
# portal, the bot, the bootstrap scripts) times out. host.docker.internal
# does work, so redirect TCP traffic for the server address to it.
#
# Runs in the Docker VM's network namespace (network_mode: host, NET_ADMIN).
# The legacy iptables backend is required: the nft backend accepts the rule
# but Docker Desktop's VM does not honour it. The rule is re-applied every
# 30 s so it survives a Docker restart, and removed on shutdown.
set -eu

: "${SERVER_IP:?SERVER_IP is required}"
GW=$(getent hosts host.docker.internal | awk '{print $1; exit}')
[ -n "$GW" ] || { echo "hairpin: cannot resolve host.docker.internal" >&2; exit 1; }

RULE="-d $SERVER_IP -p tcp -j DNAT --to-destination $GW"
# shellcheck disable=SC2086
apply() { iptables-legacy -t nat -C PREROUTING $RULE 2>/dev/null || iptables-legacy -t nat -I PREROUTING 1 $RULE; }
# shellcheck disable=SC2086
remove() { iptables-legacy -t nat -D PREROUTING $RULE 2>/dev/null || true; }
trap 'remove; exit 0' TERM INT

echo "hairpin: redirecting $SERVER_IP -> $GW"
while true; do
    apply
    sleep 30 &
    wait $!
done
