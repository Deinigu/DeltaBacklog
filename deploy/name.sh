#!/bin/sh
# The friendly address http://deltabacklog.local, run by deltabacklog-name.service (root).
#   name.sh rules-add   send port 80 (traffic addressed to this Pi, and the Pi's own) to DeltaBacklog on 8081
#   name.sh rules-del   remove those rules (also the first version of the rule, which had no address match)
#   name.sh publish     announce deltabacklog.local with the address of the default route (mDNS); stays running
set -eu

PORT=8081
# Only traffic addressed to the Pi: forwarded packets (containers going out to port 80) are left alone.
pre() { iptables -t nat "$1" PREROUTING -p tcp --dport 80 -m addrtype --dst-type LOCAL -j REDIRECT --to-ports "$PORT"; }
out() { iptables -t nat "$1" OUTPUT -o lo -p tcp --dport 80 -j REDIRECT --to-ports "$PORT"; }
legacy() { iptables -t nat "$1" PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$PORT"; }

case "${1:-}" in
  rules-add)
    pre -C 2>/dev/null || pre -A
    out -C 2>/dev/null || out -A
    ;;
  rules-del)
    pre -D 2>/dev/null || true
    out -D 2>/dev/null || true
    legacy -D 2>/dev/null || true
    ;;
  publish)
    ADDR=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}')
    [ -n "$ADDR" ] || { echo "no network address yet" >&2; exit 1; }
    exec avahi-publish -a -R deltabacklog.local "$ADDR"
    ;;
  *) echo "usage: name.sh rules-add|rules-del|publish" >&2; exit 2 ;;
esac
