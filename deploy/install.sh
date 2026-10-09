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

STAGE=${1:?usage: install.sh <stage-dir>}
APP=/opt/deltabacklog
DATA=/var/lib/deltabacklog
BACKUPS=/var/backups/deltabacklog

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
  install -m 600 -o root -g root "$STAGE/env" /etc/deltabacklog.env
  echo "settings installed to /etc/deltabacklog.env"
elif [ ! -f /etc/deltabacklog.env ]; then
  printf 'LISTEN_ADDR=:8081\nDATA_DIR=%s\nGOMEMLIMIT=96MiB\n#ALLOWED_HOSTS=\n' "$DATA" > /etc/deltabacklog.env
  chmod 600 /etc/deltabacklog.env
  echo "created a default /etc/deltabacklog.env (add the IGDB / SteamGridDB keys to enable lookups)"
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
