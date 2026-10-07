#!/bin/sh
# Nightly backup of the DeltaBacklog database: one consistent copy per day, the last 14 days kept.
# Covers are not backed up: they can be downloaded again from IGDB / SteamGridDB.
# Restore: stop the service, copy a backup over /var/lib/deltabacklog/app.db (owner deltabacklog), start it.
set -eu

DEST=/var/backups/deltabacklog
KEEP_DAYS=14

mkdir -p "$DEST"
FILE="$DEST/app-$(date +%Y-%m-%d).db"
rm -f "$FILE"   # running it twice in a day replaces that day's copy

/opt/deltabacklog/deltabacklog backup "$FILE"

# prune old copies (and any leftover partial file from an interrupted run)
find "$DEST" -name 'app-*.db' -mtime +"$KEEP_DAYS" -delete
find "$DEST" -name '*.partial' -mmin +60 -delete
