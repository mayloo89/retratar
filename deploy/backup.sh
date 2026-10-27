#!/usr/bin/env bash
# Nightly encrypted backup of the database and the photos volume.
#
# Each run writes two files, encrypted to a public key so the host cannot read
# its own backups, into BACKUP_DIR:
#   retratar-db-YYYY-MM-DD.dump.gpg      pg_dump -Fc of the database
#   retratar-photos-YYYY-MM-DD.tgz.gpg   tar.gz of the photos volume
# then copies them to an rclone remote (if set), prunes anything older than
# BACKUP_KEEP_DAYS locally and remotely, and pings a dead-man's-switch URL.
#
# Nothing old is deleted and no ping is sent unless every earlier step worked.
# The remote is only ever written with `rclone copy` and pruned by age, never
# `rclone sync`: sync mirrors deletions, so an emptied local directory would
# wipe the remote copies.
#
# CONFIGURATION (environment variables):
#   BACKUP_GPG_RECIPIENT  required. Key ID or fingerprint of the public key to
#                         encrypt to. It must already be in the running user's
#                         gpg keyring. The private key is never needed here.
#   BACKUP_DIR            default $HOME/retratar-backups. Local directory for
#                         the encrypted files. Created 0700 if missing.
#   BACKUP_REMOTE         default empty. rclone "remote:path". Empty means local
#                         only. rclone reads its own config file; no secret is
#                         passed on a command line.
#   BACKUP_KEEP_DAYS      default 7. Retention, locally and on the remote.
#   BACKUP_PING_URL       default empty. Fetched only after full success.
#   PG_CONTAINER          default retratar-postgres-1.
#   PG_USER, PG_DB        default retratar, retratar.
#   PHOTOS_VOLUME         default retratar_photos.
#
# Needs: bash, docker, gpg, flock, curl (if pinging), rclone (if BACKUP_REMOTE).
# Setup, schedule and restore: deploy/RUNBOOK.md, section Backups.

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-$HOME/retratar-backups}"
BACKUP_REMOTE="${BACKUP_REMOTE:-}"
BACKUP_KEEP_DAYS="${BACKUP_KEEP_DAYS:-7}"
BACKUP_PING_URL="${BACKUP_PING_URL:-}"
PG_CONTAINER="${PG_CONTAINER:-retratar-postgres-1}"
PG_USER="${PG_USER:-retratar}"
PG_DB="${PG_DB:-retratar}"
PHOTOS_VOLUME="${PHOTOS_VOLUME:-retratar_photos}"

die() {
  echo "backup: $*" >&2
  exit 1
}

# 1. Prerequisites.
[[ -n "${BACKUP_GPG_RECIPIENT:-}" ]] || die "BACKUP_GPG_RECIPIENT is not set"
[[ "$BACKUP_KEEP_DAYS" =~ ^[1-9][0-9]*$ ]] || die "BACKUP_KEEP_DAYS must be a positive integer"
command -v gpg >/dev/null || die "gpg is not on PATH"
gpg --list-keys "$BACKUP_GPG_RECIPIENT" >/dev/null 2>&1 ||
  die "no gpg key matches BACKUP_GPG_RECIPIENT in this user's keyring"
command -v docker >/dev/null || die "docker is not on PATH"
command -v flock >/dev/null || die "flock is not on PATH"
if [[ -n "$BACKUP_REMOTE" ]]; then
  command -v rclone >/dev/null || die "BACKUP_REMOTE is set but rclone is not on PATH"
fi
if [[ -n "$BACKUP_PING_URL" ]]; then
  command -v curl >/dev/null || die "BACKUP_PING_URL is set but curl is not on PATH"
fi

# docker run -v creates a missing volume, which would archive an empty one and
# report success. Refuse both a missing volume and a missing container.
docker volume inspect "$PHOTOS_VOLUME" >/dev/null 2>&1 ||
  die "docker volume $PHOTOS_VOLUME does not exist (PHOTOS_VOLUME)"
docker inspect "$PG_CONTAINER" >/dev/null 2>&1 ||
  die "docker container $PG_CONTAINER does not exist (PG_CONTAINER)"

mkdir -p "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"

# 2. One run at a time. fd 9 stays open, and so the lock held, until exit.
exec 9>"$BACKUP_DIR/.lock"
flock -n 9 || die "another backup is running (lock held on $BACKUP_DIR/.lock)"

# Temp files live in BACKUP_DIR so the final rename never crosses filesystems.
# Their names match no pattern used below, so nothing copies or prunes them.
tmp_db=""
tmp_photos=""
cleanup() {
  rm -f -- "$tmp_db" "$tmp_photos"
}
trap cleanup EXIT

umask 077
tmp_db="$(mktemp "$BACKUP_DIR/.tmp-db.XXXXXX")"
tmp_photos="$(mktemp "$BACKUP_DIR/.tmp-photos.XXXXXX")"

encrypt() {
  gpg --batch --yes --trust-model always --encrypt --recipient "$BACKUP_GPG_RECIPIENT" "$@"
}

# 3. Database. pipefail makes a failed pg_dump fail the pipeline even though
# gpg happily encrypts an empty stream.
docker exec "$PG_CONTAINER" pg_dump -U "$PG_USER" -d "$PG_DB" -Fc |
  encrypt --output "$tmp_db" ||
  die "database dump failed"

# 4. Photos, as the app's uid so the 0640 files are readable without root, and
# streamed to stdout so no root-owned file lands on the host.
docker run --rm --user 65532:65532 --network none -v "$PHOTOS_VOLUME":/p:ro alpine:3 \
  tar czf - -C /p . |
  encrypt --output "$tmp_photos" ||
  die "photos archive failed"

# 5. Verify: non-empty, and a complete encrypted message rather than a truncated one.
for f in "$tmp_db" "$tmp_photos"; do
  [[ -s "$f" ]] || die "$(basename "$f") is empty"
  gpg --batch --list-packets "$f" >/dev/null 2>&1 || die "$(basename "$f") is not a valid encrypted file"
done

# 6. Publish under the final names. mv within one directory is atomic.
day="$(date +%F)"
db_file="retratar-db-$day.dump.gpg"
photos_file="retratar-photos-$day.tgz.gpg"
mv -f -- "$tmp_db" "$BACKUP_DIR/$db_file"
mv -f -- "$tmp_photos" "$BACKUP_DIR/$photos_file"

# 7. Off-host copy. copy, never sync.
if [[ -n "$BACKUP_REMOTE" ]]; then
  rclone copy "$BACKUP_DIR" "$BACKUP_REMOTE" --include 'retratar-*.gpg' ||
    die "rclone copy to the remote failed"
fi

# 8. Prune, only now that the new backup exists, verified and copied.
# find -mtime counts whole days rounded down: +6 matches files 7 days old or more.
# --drive-use-trash=false deletes for good: by default Drive's Trash keeps a
# deleted file 30 more days. It is a global rclone flag; other backends ignore it.
find "$BACKUP_DIR" -maxdepth 1 -name 'retratar-*.gpg' -mtime +$((BACKUP_KEEP_DAYS - 1)) -delete ||
  die "local prune failed"
if [[ -n "$BACKUP_REMOTE" ]]; then
  rclone delete "$BACKUP_REMOTE" --include 'retratar-*.gpg' --min-age "${BACKUP_KEEP_DAYS}d" --drive-use-trash=false ||
    die "remote prune failed"
fi

# 9. Dead-man's switch: only a full success reaches here.
if [[ -n "$BACKUP_PING_URL" ]]; then
  curl -fsS -m 10 --retry 3 -o /dev/null "$BACKUP_PING_URL" || die "ping failed"
fi

# 10. Summary.
db_size="$(wc -c <"$BACKUP_DIR/$db_file" | tr -d ' ')"
photos_size="$(wc -c <"$BACKUP_DIR/$photos_file" | tr -d ' ')"
if [[ -n "$BACKUP_REMOTE" ]]; then
  where="copied to remote"
else
  where="local only, no remote"
fi
echo "backup ok: $db_file ${db_size}B, $photos_file ${photos_size}B, $where"
