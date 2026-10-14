#!/bin/bash
# Back up the ginadmin database. Run by ginadmin-backup.service; see
# deploy/OPERATIONS.md section 1 for installation.
#
# Settings come from /etc/ginadmin/backup.env (override with BACKUP_ENV):
#   DB_TYPE     mysql or postgres                       (default mysql)
#   DB_NAME     database name                           (default ginadmin)
#   BACKUP_DIR  where dumps are written                 (default /var/backups/ginadmin)
#   KEEP_DAYS   delete dumps older than this many days  (default 14)
#   AFTER_BACKUP  optional command run with the new file as $1, e.g. to copy
#               it to another machine; a failure fails the backup
# Credentials are not stored here: MySQL reads /root/.ginadmin-my.cnf,
# PostgreSQL reads PGHOST, PGUSER and ~/.pgpass.
#
# A dump is written to a temporary file, checked, then renamed, so a failed
# run never leaves a truncated file that looks like a backup. The exit
# status is non-zero on any failure, which marks the systemd unit failed.
set -euo pipefail
umask 077

BACKUP_ENV=${BACKUP_ENV:-/etc/ginadmin/backup.env}
if [ -f "$BACKUP_ENV" ]; then
	# Export everything, so PGHOST and PGUSER reach pg_dump.
	set -a
	# shellcheck source=/dev/null
	. "$BACKUP_ENV"
	set +a
fi
DB_TYPE=${DB_TYPE:-mysql}
DB_NAME=${DB_NAME:-ginadmin}
BACKUP_DIR=${BACKUP_DIR:-/var/backups/ginadmin}
KEEP_DAYS=${KEEP_DAYS:-14}
MYSQL_DEFAULTS=${MYSQL_DEFAULTS:-/root/.ginadmin-my.cnf}

log() { echo "ginadmin-backup: $*"; }

mkdir -p "$BACKUP_DIR"
stamp=$(date +%Y%m%d-%H%M%S)

case "$DB_TYPE" in
mysql)
	final="$BACKUP_DIR/$DB_NAME-$stamp.sql.gz"
	tmp="$final.partial"
	trap 'rm -f "$tmp"' EXIT
	# --single-transaction: a consistent snapshot without locking tables.
	# --no-tablespaces: no PROCESS privilege needed.
	mysqldump --defaults-extra-file="$MYSQL_DEFAULTS" --single-transaction --no-tablespaces \
		--routines --triggers "$DB_NAME" | gzip >"$tmp"
	gzip -t "$tmp"
	# mysqldump writes this line last; without it the dump was cut short.
	gzip -dc "$tmp" | tail -n 1 | grep -q '^-- Dump completed' ||
		{ log "dump of $DB_NAME is incomplete"; exit 1; }
	;;
postgres)
	final="$BACKUP_DIR/$DB_NAME-$stamp.dump"
	tmp="$final.partial"
	trap 'rm -f "$tmp"' EXIT
	pg_dump -Fc -d "$DB_NAME" -f "$tmp"
	# Reading the table of contents checks the archive is complete.
	pg_restore -l "$tmp" >/dev/null
	;;
*)
	log "unsupported DB_TYPE '$DB_TYPE', use mysql or postgres"
	exit 1
	;;
esac

mv "$tmp" "$final"
trap - EXIT
log "wrote $final ($(du -h "$final" | cut -f1))"

if [ -n "${AFTER_BACKUP:-}" ]; then
	$AFTER_BACKUP "$final"
	log "AFTER_BACKUP succeeded"
fi

# Only this script's own files are removed, and never the one just written.
find "$BACKUP_DIR" -maxdepth 1 -type f \
	\( -name "$DB_NAME-*.sql.gz" -o -name "$DB_NAME-*.dump" -o -name "$DB_NAME-*.partial" \) \
	-mtime +"$KEEP_DAYS" ! -path "$final" -print -delete |
	sed 's/^/ginadmin-backup: removed /'
