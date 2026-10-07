#!/bin/sh
# Installs or updates DeltaBacklog on the Raspberry Pi. Run as root, from the folder `deploy.sh` uploaded.
#
#   sudo ./install.sh <stage-dir>
#
# <stage-dir> contains: deltabacklog (the armv7 binary), the .service/.timer files, backup.sh, and optionally
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
  printf 'LISTEN_ADDR=:8081\nDATA_DIR=%s\nGOMEMLIMIT=96MiB\n' "$DATA" > /etc/deltabacklog.env
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

# 5. the program, replaced atomically
CHANGED=0
if ! cmp -s "$STAGE/deltabacklog" "$APP/deltabacklog" 2>/dev/null; then
  install -m 755 "$STAGE/deltabacklog" "$APP/deltabacklog.new"
  mv -f "$APP/deltabacklog.new" "$APP/deltabacklog"
  CHANGED=1
fi
install -m 755 "$STAGE/backup.sh" "$APP/backup.sh"

# 6. systemd units
install -m 644 "$STAGE/deltabacklog.service" "$STAGE/deltabacklog-backup.service" "$STAGE/deltabacklog-backup.timer" /etc/systemd/system/
systemctl daemon-reload
systemctl enable deltabacklog.service deltabacklog-backup.timer >/dev/null 2>&1
if systemctl is-active --quiet deltabacklog.service; then
  [ "$CHANGED" = 1 ] || [ -f "$STAGE/env" ] && systemctl restart deltabacklog.service
else
  systemctl start deltabacklog.service
fi
systemctl start deltabacklog-backup.timer

# 7. wait for it to answer
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
echo "DeltaBacklog is running (enabled at boot): http://$(hostname -I | awk '{print $1}'):8081"
