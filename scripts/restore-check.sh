#!/bin/sh
set -eu
umask 077
# PGHOST points only at the isolated verification service in the compose profile.
test "${PGDATABASE:-}" = vivid_restore_test || { echo 'Expected isolated vivid_restore_test database' >&2; exit 1; }
archive=$(find /backups -maxdepth 1 -name 'backup-*.dump' -type f | sort | tail -n 1)
test -n "$archive"
sha256sum -c "$archive.sha256"
pg_restore --clean --if-exists --exit-on-error --no-owner --no-privileges --dbname="$PGDATABASE" "$archive"
psql --set=ON_ERROR_STOP=1 --command="SELECT count(*) AS orders FROM orders; SELECT count(*) AS accounts FROM accounts;"
echo 'Restore verification completed'
