#!/bin/sh
# Installs or updates DeltaBacklog on the Raspberry Pi. Run as root, from the folder `deploy.sh` uploaded.
#
#   sudo ./install.sh <stage-dir>
#
# <stage-dir> contains: deltabacklog (the armv7 binary), the .service/.timer files (including deltabacklog-name.service), backup.sh, name.sh, and optionally
#   env       the settings file; installed to /etc/deltabacklog.env (root only) when given, kept otherwise
#   seed/     app.db and covers/ for a first install; only used when no database exists yet
# It is safe to run again: data is never overwritten, the service is restarted only if the program changed.
set -eu

APP=/opt/deltabacklog
DATA=/var/lib/deltabacklog
BACKUPS=/var/backups/deltabacklog

# env_value <file> <KEY>: the last value of KEY in a settings file (quotes removed), or nothing
env_value() { sed -n "s/^$2=//p" "$1" 2>/dev/null | tail -n 1 | tr -d "\"'"; }

# on_other_mount <path>: true when the part of path that exists lives on a mount other than /. An unmounted drive
# leaves only an empty mount point on the SD card, which is on /: nothing may be created there.
on_other_mount() {
  p=$1
  while [ ! -d "$p" ] && [ "$p" != / ]; do p=$(dirname "$p"); done
  t=$(findmnt -n -o TARGET -T "$p" 2>/dev/null) || return 1
  [ -n "$t" ] && [ "$t" != / ]
}

# shots_dropin <screenshots-dir> <data-dir> <unit-dir>: let the hardened units write to a screenshots folder outside the
# data folder. The leading "-" makes a missing path (drive absent) harmless: the units still start. No dependency on the
# mount is added on purpose: the app must run, and show its message, without the drive. Writes nothing and removes old
# drop-ins when the folder is empty or inside the data folder.
shots_dropin() {
  dir=$1 data=$2 units=$3
  case "$dir" in "$data"|"$data"/*) dir= ;; esac
  for u in deltabacklog deltabacklog-backup; do
    f=$units/$u.service.d/screenshots.conf
    if [ -z "$dir" ]; then rm -f "$f"; rmdir "$units/$u.service.d" 2>/dev/null || true; continue; fi
    rw=$dir
    [ "$u" = deltabacklog-backup ] && rw=$(dirname "$dir")   # the backup also writes <parent>/backups
    mkdir -p "$units/$u.service.d"
    printf '[Service]
ReadWritePaths=-%s
' "$rw" > "$f"
  done
}

# Sourced by the tests (INSTALL_LIB=1): define the functions and stop.
if [ "${INSTALL_LIB:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

STAGE=${1:?usage: install.sh <stage-dir>}

[ "$(id -u)" = 0 ] || { echo "run as root (sudo)"; exit 1; }

# 1. a dedicated account with no login and no shell
if ! id deltabacklog >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin deltabacklog
  echo "created user deltabacklog"
fi

# 2. folders
install -d -m 755 "$APP"
install -d -m 750 -o deltabacklog -g deltabacklog "$DATA" "$DATA/covers" "$BACKUPS"

# 3. settings (secrets: readable by root only; systemd reads it before dropping privileges)
if [ -f "$STAGE/env" ]; then
  OLD_SHOTS=$(env_value /etc/deltabacklog.env SCREENSHOTS_DIR)
  install -m 600 -o root -g root "$STAGE/env" /etc/deltabacklog.env
  # the deploy's settings file does not carry the Pi-only drive setting: keep it
  if [ -n "$OLD_SHOTS" ] && [ -z "$(env_value /etc/deltabacklog.env SCREENSHOTS_DIR)" ]; then
    echo "SCREENSHOTS_DIR=$OLD_SHOTS" >> /etc/deltabacklog.env
  fi
  echo "settings installed to /etc/deltabacklog.env"
elif [ ! -f /etc/deltabacklog.env ]; then
  printf 'LISTEN_ADDR=:8081\nDATA_DIR=%s\nGOMEMLIMIT=96MiB\n#ALLOWED_HOSTS=\n' "$DATA" > /etc/deltabacklog.env
  chmod 600 /etc/deltabacklog.env
  echo "created a default /etc/deltabacklog.env (add the IGDB / SteamGridDB keys to enable lookups)"
fi

# 3b. screenshots on another drive (SCREENSHOTS_DIR in the settings). The folders are created, owned by the service
#     user, only when the drive is really mounted; otherwise nothing is created and the app says "screenshot storage
#     not available" until it is.
SHOTS=$(env_value /etc/deltabacklog.env SCREENSHOTS_DIR)
shots_dropin "$SHOTS" "$DATA" /etc/systemd/system
if [ -n "$SHOTS" ]; then
  case "$SHOTS" in
    "$DATA"|"$DATA"/*) ;;
    *)
      if on_other_mount "$SHOTS"; then
        install -d -m 750 -o deltabacklog -g deltabacklog "$SHOTS" "$(dirname "$SHOTS")/backups"
        echo "screenshot folders ready: $SHOTS"
      else
        echo "warning: $SHOTS is not on a separate mounted drive; nothing created (mount the drive, then run the install again)"
      fi ;;
  esac
fi

# 4. first-install data; never overwrite an existing database
if [ -d "$STAGE/seed" ] && [ ! -f "$DATA/app.db" ]; then
  install -m 640 -o deltabacklog -g deltabacklog "$STAGE/seed/app.db" "$DATA/app.db"
  if [ -d "$STAGE/seed/covers" ]; then
    cp -r "$STAGE/seed/covers/." "$DATA/covers/"
    chown -R deltabacklog:deltabacklog "$DATA/covers"
  fi
  echo "seeded the database and $(ls "$DATA/covers" | wc -l) covers"
elif [ -d "$STAGE/seed" ]; then
  echo "a database already exists: seed data ignored"
fi

# 5. the program, replaced atomically. A new program may migrate the database when it starts, so the database is
#    copied first (as the service user, so no file in the data folder ends up owned by root). `backup` never migrates.
CHANGED=0
if ! cmp -s "$STAGE/deltabacklog" "$APP/deltabacklog" 2>/dev/null; then
  install -m 755 "$STAGE/deltabacklog" "$APP/deltabacklog.new"
  if [ -f "$DATA/app.db" ]; then
    PRE="$BACKUPS/app-before-upgrade-$(date +%Y%m%d-%H%M%S).db"
    if ! (cd "$DATA" && runuser -u deltabacklog -- env DATA_DIR="$DATA" "$APP/deltabacklog.new" backup "$PRE"); then
      rm -f "$APP/deltabacklog.new"
      echo "the backup before the upgrade failed: the program was not updated"
      exit 1
    fi
    echo "backup before the upgrade: $PRE"
  fi
  mv -f "$APP/deltabacklog.new" "$APP/deltabacklog"
  CHANGED=1
fi
# name.sh holds the iptables rules: when it changes, the OLD script removes its rules before the new one is installed
NAME_CHANGED=0
if ! cmp -s "$STAGE/name.sh" "$APP/name.sh" 2>/dev/null; then
  NAME_CHANGED=1
  [ -x "$APP/name.sh" ] && "$APP/name.sh" rules-del || true
fi
install -m 755 "$STAGE/backup.sh" "$STAGE/name.sh" "$APP/"

# 6. systemd units
install -m 644 "$STAGE/deltabacklog.service" "$STAGE/deltabacklog-backup.service" "$STAGE/deltabacklog-backup.timer" /etc/systemd/system/
cmp -s "$STAGE/deltabacklog-name.service" /etc/systemd/system/deltabacklog-name.service 2>/dev/null || NAME_CHANGED=1
install -m 644 "$STAGE/deltabacklog-name.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable deltabacklog.service deltabacklog-backup.timer >/dev/null 2>&1
if systemctl is-active --quiet deltabacklog.service; then
  [ "$CHANGED" = 1 ] || [ -f "$STAGE/env" ] && systemctl restart deltabacklog.service
else
  systemctl start deltabacklog.service
fi
systemctl start deltabacklog-backup.timer

# 7. the friendly address http://deltabacklog.local (mDNS name + port 80 -> 8081). Optional: a failure here
#    must not fail the install, the app itself is already running.
name_setup() {
  if ! command -v avahi-publish >/dev/null 2>&1 || ! command -v iptables >/dev/null 2>&1 || ! systemctl cat avahi-daemon.service >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq && apt-get install -y -qq avahi-daemon avahi-utils iptables || return 1
  fi
  if ss -ltnH 'sport = :80' 2>/dev/null | grep -q .; then
    echo "note: something already listens on port 80 on this Pi; traffic addressed to the Pi on port 80 now goes to DeltaBacklog"
  fi
  systemctl enable deltabacklog-name.service >/dev/null 2>&1 || return 1
  # an older unit never removed its rules (the first one also caught forwarded traffic): clear them before restarting
  if [ "$NAME_CHANGED" = 1 ]; then "$APP/name.sh" rules-del; systemctl restart deltabacklog-name.service; else systemctl start deltabacklog-name.service; fi
}
name_setup || echo "warning: http://deltabacklog.local was not set up; use http://<address>:8081"

# 8. wait for it to answer
i=0
until curl -fsS -m 3 http://127.0.0.1:8081/healthz >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 20 ]; then
    echo "the service did not answer; recent log:"
    journalctl -u deltabacklog.service -n 20 --no-pager
    exit 1
  fi
  sleep 1
done
echo "DeltaBacklog is running (enabled at boot): http://deltabacklog.local or http://$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}'):8081"
