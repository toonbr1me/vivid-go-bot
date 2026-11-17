#!/bin/sh
set -eu
umask 077
: "${PGHOST:=db}" "${PGPORT:=5432}" "${PGUSER:=${POSTGRES_USER:?}}" "${PGDATABASE:=${POSTGRES_DB:?}}" "${PGPASSWORD:=${POSTGRES_PASSWORD:?}}"
export PGHOST PGPORT PGUSER PGDATABASE PGPASSWORD
mkdir -p /backups
while :; do
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  temporary="/backups/backup-$stamp.dump.part"
  archive="/backups/backup-$stamp.dump"
  if pg_dump --format=custom --file="$temporary" && pg_restore --list "$temporary" >/dev/null; then
    mv "$temporary" "$archive"
    sha256sum "$archive" > "$archive.sha256"
    date -u +%s > /backups/last-success
    find /backups -maxdepth 1 -name 'backup-*.dump*' -type f -mtime +14 -delete
    echo "Backup completed: $stamp"
  else
    rm -f "$temporary"
    echo "Backup failed" >&2
  fi
  sleep "${BACKUP_INTERVAL_SECONDS:-21600}"
done
