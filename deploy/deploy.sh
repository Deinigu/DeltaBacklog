#!/usr/bin/env bash
# Builds DeltaBacklog for the Raspberry Pi (armv7) and installs it there over ssh.
#
#   deploy/deploy.sh [--seed <dir>] [--env <file>]
#
#   --seed <dir>   first install: <dir> holds app.db and covers/ (ignored when the Pi already has a database)
#   --env <file>   a .env with IGDB_CLIENT_ID, IGDB_CLIENT_SECRET, STEAMGRIDDB_API_KEY; installed as the
#                  service's settings (root only on the Pi). Without it the existing settings are kept.
#   DEPLOY_HOST    the ssh host name (default: raspberry)
#
# Go must be on PATH. Nothing is built or run on the Pi.
set -euo pipefail

HOST=${DEPLOY_HOST:-raspberry}
SEED=""
ENVFILE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --seed) SEED=$2; shift 2 ;;
    --env)  ENVFILE=$2; shift 2 ;;
    *) echo "unknown option: $1"; exit 2 ;;
  esac
done

cd "$(dirname "$0")/.."
ROOT=$(pwd)
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT
STAGE=/tmp/deltabacklog-deploy

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
echo "==> building $VERSION for linux/arm (armv7)"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/deltabacklog" ./cmd/deltabacklog
ls -l "$OUT/deltabacklog" | awk '{printf "    binary: %.1f MB\n", $5/1048576}'

cp deploy/deltabacklog.service deploy/deltabacklog-backup.service deploy/deltabacklog-backup.timer deploy/deltabacklog-name.service deploy/name.sh deploy/backup.sh deploy/install.sh "$OUT/"

if [ -n "$ENVFILE" ]; then
  {
    echo "LISTEN_ADDR=:8081"
    echo "DATA_DIR=/var/lib/deltabacklog"
    echo "GOMEMLIMIT=96MiB"
    grep -E '^(IGDB_CLIENT_ID|IGDB_CLIENT_SECRET|STEAMGRIDDB_API_KEY)=' "$ENVFILE" || true
  } > "$OUT/env"
fi

echo "==> uploading to $HOST"
ssh "$HOST" "rm -rf $STAGE && mkdir -p $STAGE"
scp -q "$OUT"/* "$HOST:$STAGE/"
if [ -n "$SEED" ]; then
  [ -f "$SEED/app.db" ] || { echo "$SEED/app.db not found"; exit 1; }
  ssh "$HOST" "mkdir -p $STAGE/seed"
  scp -q "$SEED/app.db" "$HOST:$STAGE/seed/app.db"
  [ -d "$SEED/covers" ] && tar -C "$SEED" -cf - covers | ssh "$HOST" "tar -C $STAGE/seed -xf -"
fi

echo "==> installing"
ssh "$HOST" "chmod 700 $STAGE && chmod +x $STAGE/install.sh && sudo $STAGE/install.sh $STAGE; rc=\$?; rm -rf $STAGE; exit \$rc"
echo "==> done"
