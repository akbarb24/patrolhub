#!/bin/sh
# Daily backup of the database and the photo folder. Run it from cron on the server, e.g.
#   15 2 * * * cd /srv/patrolhub && ./deploy/backup.sh >> /var/log/patrolhub-backup.log 2>&1
#
# Restore test (do this once before going live):
#   createdb restore_test && pg_restore -d restore_test /backup/patrolhub-YYYYMMDD.dump
set -e

STAMP=$(date +%Y%m%d)
BACKUP_DIR=${BACKUP_DIR:-/backup}
APP_DIR=${APP_DIR:-/srv/patrolhub}

docker compose exec -T db pg_dump -U patrolhub -Fc patrolhub > "$BACKUP_DIR/patrolhub-$STAMP.dump"
tar -czf "$BACKUP_DIR/photos-$STAMP.tar.gz" -C "$APP_DIR" data/photos
find "$BACKUP_DIR" -type f -mtime +30 -delete
