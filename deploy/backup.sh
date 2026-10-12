#!/bin/sh
# Nightly backup of DeltaBacklog.
#
#   1. the database: one consistent copy per day in /var/backups/deltabacklog, the last 14 days kept;
#   2. when SCREENSHOTS_DIR is set and its drive is there: the same copy again in <SCREENSHOTS_DIR>/../backups,
#      so a dead SD card does not take the database with it (same 14 days);
#   3. screenshots: files missing from /var/backups/deltabacklog/screenshots are copied there, never deleted, and the
#      copy stops before the SD card drops under SHOTS_MIN_FREE_GB (default 5) free (checked per file by the program).
#      The originals live on the drive and this copy on the SD card: two devices. Covers are not backed up: they can be
#      downloaded again.
#
# Steps 2 and 3 never fail the database backup: a missing drive or a full SD card is logged and skipped.
# Restore: stop the service, copy a backup over /var/lib/deltabacklog/app.db (owner deltabacklog), start it.
set -eu

DEST=${BACKUP_DEST:-/var/backups/deltabacklog}
KEEP_DAYS=14
APP=${APP_DIR:-/opt/deltabacklog}
MIN_FREE_GB=${SHOTS_MIN_FREE_GB:-5}

# backup_shots <src> <dst>: copy new screenshot files. The binary checks the free space of the backup disk before every
# file and stops copying when the next one would leave less than MIN_FREE_GB free there.
backup_shots() {
  src=$1 dst=$2
  [ -d "$src" ] || { echo "screenshots: $src not available (drive not mounted?), skipped"; return 0; }
  mkdir -p "$dst"
  SCREENSHOTS_DIR=$src "$APP/deltabacklog" backup-screenshots --min-free-gb "$MIN_FREE_GB" "$dst"
}

# keep_copy <file> <dir>: copy a database backup into dir (an existing folder on the other drive) and prune old ones
keep_copy() {
  [ -d "$2" ] || { echo "database copy: $2 not available, skipped"; return 0; }
  cp "$1" "$2/$(basename "$1").partial" && mv -f "$2/$(basename "$1").partial" "$2/$(basename "$1")"
  find "$2" -name 'app-*.db' -mtime +"$KEEP_DAYS" -delete
  find "$2" -name '*.partial' -mmin +60 -delete
}

# Sourced by the tests (BACKUP_LIB=1): define the functions and stop.
if [ "${BACKUP_LIB:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

mkdir -p "$DEST"
FILE="$DEST/app-$(date +%Y-%m-%d).db"
rm -f "$FILE"   # running it twice in a day replaces that day's copy

"$APP/deltabacklog" backup "$FILE"

# prune old copies (and any leftover partial file from an interrupted run)
find "$DEST" -name 'app-*.db' -mtime +"$KEEP_DAYS" -delete
find "$DEST" -name '*.partial' -mmin +60 -delete

if [ -n "${SCREENSHOTS_DIR:-}" ]; then
  keep_copy "$FILE" "$(dirname "$SCREENSHOTS_DIR")/backups" || echo "database copy to the drive failed"
fi
backup_shots "${SCREENSHOTS_DIR:-${DATA_DIR:-/var/lib/deltabacklog}/screenshots}" "$DEST/screenshots" || echo "screenshot backup failed"
