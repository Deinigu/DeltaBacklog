#!/bin/sh
# Nightly backup of DeltaBacklog.
#
#   1. the database: one consistent copy per day in /var/backups/deltabacklog, the last 14 days kept;
#   2. when SCREENSHOTS_DIR is set and its drive is there: the same copy again in <SCREENSHOTS_DIR>/../backups,
#      so a dead SD card does not take the database with it (same 14 days);
#   3. screenshots: files missing from /var/backups/deltabacklog/screenshots are copied there, never deleted, but only
#      while the SD card keeps SHOTS_MIN_FREE_GB (default 5) free after the copy. The originals live on the drive and
#      this copy on the SD card: two devices. Covers are not backed up: they can be downloaded again.
#
# Steps 2 and 3 never fail the database backup: a missing drive or a full SD card is logged and skipped.
# Restore: stop the service, copy a backup over /var/lib/deltabacklog/app.db (owner deltabacklog), start it.
set -eu

DEST=${BACKUP_DEST:-/var/backups/deltabacklog}
KEEP_DAYS=14
APP=${APP_DIR:-/opt/deltabacklog}
MIN_FREE_GB=${SHOTS_MIN_FREE_GB:-5}

# free_kb <path>: free space (KiB) on the filesystem that holds path
free_kb() { df -Pk "$1" | awk 'NR==2 {print $4}'; }

# dir_kb <dir>: size of a folder in KiB (0 when it does not exist)
dir_kb() { du -sk "$1" 2>/dev/null | awk '{print $1}' | grep . || echo 0; }

# room_for <path> <need_kb> <min_free_gb>: true when copying need_kb to path still leaves min_free_gb free
room_for() { [ $(($(free_kb "$1") - $2)) -ge $(($3 * 1048576)) ]; }

# backup_shots <src> <dst>: copy new screenshot files, unless the SD card would drop below the free-space margin
backup_shots() {
  src=$1 dst=$2
  [ -d "$src" ] || { echo "screenshots: $src not available (drive not mounted?), skipped"; return 0; }
  mkdir -p "$dst"
  need=$(($(dir_kb "$src") - $(dir_kb "$dst")))
  [ "$need" -gt 0 ] || need=0
  if ! room_for "$dst" "$need" "$MIN_FREE_GB"; then
    echo "screenshots: skipped, copying about $((need / 1024)) MB would leave less than $MIN_FREE_GB GB free on the backup disk"
    return 0
  fi
  SCREENSHOTS_DIR=$src "$APP/deltabacklog" backup-screenshots "$dst"
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
