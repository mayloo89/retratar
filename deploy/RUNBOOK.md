# Deploy runbook

Operational companion to the setup instructions, which live in the header
comments of the files they belong to and are not repeated here:

- `deploy/docker-compose.yml` — first-time Postgres + app bring-up, TLS
  enablement, migrations
- `deploy/nginx/retrat.ar.conf` and `retratar.com.ar.conf` — Cloudflare zone
  setup, Origin CA certificates, nginx install
- `docs/decisions/0004-*` and `0005-*` — why the topology is what it is

This file covers the things you only learn the second time: how to re-deploy,
how to tell a real outage from a network in the way, and the traps that have
already cost an afternoon each.

## The box

The host is reachable only from its LAN — there is no public SSH and no VPN —
so everything below needs LAN access. Commands assume a shell on the host,
inside the repo checkout.

## Procedure A — re-deploy from `develop`

Do this first, and in this order: nginx before the app, because a broken nginx
config is the failure that leaves you with nothing serving at all.

    cd <checkout>
    git fetch origin
    git checkout develop && git pull --ff-only

From this release, `deploy/retratar.env` must set `OPERATOR_NAME` and
`CONTACT_EMAIL`, or the app refuses to start.

**1. nginx configs. Install both, in one step.**

    sudo cp deploy/nginx/retrat.ar.conf          /etc/nginx/conf.d/
    sudo cp deploy/nginx/retratar.com.ar.conf    /etc/nginx/conf.d/
    sudo cp deploy/nginx/cloudflare-ips.conf.inc /etc/nginx/conf.d/
    sudo nginx -t && sudo systemctl reload nginx

Both files, always. Since PR #10, `retrat.ar.conf` uses `$retratar_client_ip`,
whose `map` is defined only in `retratar.com.ar.conf` — the two share one
`http` context and the map must not be declared twice. Copy only one and
`nginx -t` fails with `unknown "retratar_client_ip" variable`, the reload is
rejected, and nginx keeps serving its previous config: the command looks like
it did something and nothing changed.

Never `systemctl reload` without `nginx -t &&` in front of it.

**2. App.**

    docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env \
      build --pull app
    docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env \
      run --rm app -migrate
    docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env \
      up -d app

Build first: `run` reuses whatever image already exists, so migrating before
building applies the previous release's migrations and the new app then starts
against an old schema. `--pull` refreshes the Go base image, so a cached older
toolchain can't build the release.

**3. Verify the rate limiter is actually live.** Check it rather than
assuming:

    for i in $(seq 1 8); do
      curl -s -o /dev/null -w "%{http_code} " -H 'Host: retratar.com.ar' \
        -X POST --data-urlencode 'email=you+probe@example.com' http://127.0.0.1:8082/login
    done; echo

The app routes by `Host`, so a bare loopback request reaches neither surface
(404); the header selects one. Substitute a real address of your own for
`you+probe@example.com`: an `example.com` address bounces, and a plus-address
is a separate address to the app, so the probe does not spend the sign-in
budget of the account you use.

Expect five `200` then `429` — the bucket is 5 burst, one token back every
20s. Only the first three requests send mail; the rest are absorbed by the
per-address limit. All eight returning `200` means the container is still the
old image; go back to step 2.

This hits the app on loopback, so it tests the Go limiter only. The nginx
`limit_req` layer is a separate check — make the same requests through the
public hostname from a network without DNS or TLS filtering.

## Procedure B — before calling it an outage

Some networks (corporate or ISP DNS/TLS filters) intercept these domains and
reset connections after the handshake — at first glance indistinguishable from
an origin fault. Present the certificate first:

    openssl s_client -connect <host>:443 -servername <host> </dev/null 2>/dev/null | openssl x509 -noout -issuer

If the issuer is not the expected CA for the edge, you are looking at a
middlebox, not the deploy; check from another network (e.g. a phone hotspot).
Only then work inward — app on loopback, nginx on loopback, then the Cloudflare
zone settings:

    curl -sS -i -H 'Host: example.retrat.ar' http://127.0.0.1:8082/ | head -1
    curl -sSk -i -H 'Host: example.retrat.ar' https://127.0.0.1/ | head -1

## Log rotation

Container logs rotate (`logging:` in `docker-compose.yml`, 5 x 10 MB per service). The setting applies when a container is **recreated** (`up -d`), not on a plain restart.

## Stats

Print aggregate numbers (accounts, mood changes, guestbook, photos) from the database:

    docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env run --rm app -stats

The output contains no personal data: no emails, handles, notes or entry text.

## Photos

Uploaded photos are files in the `photos` Docker volume, mounted at
`/data/photos` in the app container. The database holds only each photo's key.

The volume is backed up nightly; see [Backups](#backups).

Only allowlisted accounts can upload. Allow an account with SQL; there is no UI
for it:

    docker exec <postgres container> psql -U retratar -d retratar \
      -c "UPDATE users SET can_upload_photos = true WHERE handle = '<handle>';"

## Backups

`deploy/backup.sh` writes two files every night, encrypted to a public key so the
host cannot read its own backups:

- `retratar-db-YYYY-MM-DD.dump.gpg`: `pg_dump -Fc` of the database;
- `retratar-photos-YYYY-MM-DD.tgz.gpg`: the `photos` volume.

They are kept in `BACKUP_DIR` (default `~/retratar-backups`), copied to an rclone
remote, and both places are pruned to `BACKUP_KEEP_DAYS` days (default 7): no
backup is kept longer than that. A failure before pruning deletes nothing old and
sends no ping. A failure during pruning can leave it half done; the next
successful run finishes it. Every variable is documented in the header of the
script.

The privacy policy names the backup provider (Google Drive) and promises that
deleted data leaves the backups within 7 days. Changing the provider, or keeping
backups longer than 7 days, means changing `privacy.html` first.

Not covered: `deploy/retratar.env` and `deploy/postgres-tls/` (see Traps >
Secrets), and point-in-time recovery. A restore loses up to a day.

### One-time setup

1. **On a trusted machine, not the host**, create a key pair for backups and
   export the public key:

       gpg --quick-gen-key "retratar backups" default default never
       gpg --export --armor "retratar backups" > retratar-backups.pub

   **The private key is the only way to restore.** Export it with a passphrase
   (`gpg --export-secret-keys --armor`) and store it somewhere that is neither
   the host nor the cloud folder, for example a password manager.
2. **On the host**, import the public key and note its fingerprint:

       gpg --import retratar-backups.pub
       gpg --list-keys --fingerprint

3. **On the host**, install rclone, create a remote and test it. The remote must
   be Google Drive, because the privacy policy names it:

       rclone config
       rclone lsd <remote>:

   The script deletes expired files with `--drive-use-trash=false`, so they are
   gone at once instead of sitting 30 more days in Drive's Trash. If the Drive web
   UI ever shows retratar files in the Trash, empty it.

4. Run the script once by hand and check the result:

       BACKUP_GPG_RECIPIENT=<fingerprint> BACKUP_REMOTE=<remote>:<path> \
         deploy/backup.sh
       ls -l ~/retratar-backups
       rclone ls <remote>:<path>

   Without `BACKUP_REMOTE` it makes local copies only, and says so.

### Schedule

As the deploy user (in the `docker` group, no sudo needed), nightly. `logger`
sends the output to the journal, which already rotates:

    30 4 * * * BACKUP_GPG_RECIPIENT=<fingerprint> BACKUP_REMOTE=<remote>:<path> BACKUP_PING_URL=<url> /path/to/checkout/deploy/backup.sh 2>&1 | logger -t retratar-backup

With this pipe cron sees `logger`'s exit status, not the script's. A failure is
caught by the missing ping, so point `BACKUP_PING_URL` at a dead-man's-switch
monitor that alerts when the ping stops. The script takes its own lock, so an
overlapping run exits instead of doubling up.

Check that it ran:

    journalctl -t retratar-backup --since yesterday

### Restore

Set a restrictive umask first in every shell you decrypt in: the plaintext
files hold the whole database and every photo.

    umask 077

On the trusted machine, with the private key. This restores into a throwaway
Postgres and lists the photos; it touches nothing in production:

    gpg -d retratar-db-DATE.dump.gpg > db.dump
    docker run -d --name restore-test -e POSTGRES_PASSWORD=x postgres:18-alpine
    docker cp db.dump restore-test:/db.dump
    docker exec restore-test pg_restore -U postgres -d postgres --create --no-owner /db.dump   # creates the retratar database; no retratar role here
    docker exec restore-test psql -U postgres -d retratar -c 'SELECT count(*) FROM users;'
    gpg -d retratar-photos-DATE.tgz.gpg | tar tzf - | head

Wait a few seconds after `docker run` for Postgres to accept connections. Then
`docker rm -f restore-test` and delete `db.dump`.

**Test a restore once a month.** A backup that has never been restored isn't a
backup.

### Restore into production

The private key never goes to the host. On the trusted machine, with
`umask 077` set, decrypt both files, then copy them to the host over the LAN and
restrict them there:

    gpg -d retratar-db-DATE.dump.gpg > db.dump
    gpg -d retratar-photos-DATE.tgz.gpg > photos.tgz

    chmod 600 db.dump photos.tgz        # on the host, after copying

Paste the steps below one at a time, from the checkout on the host. **If any
step fails, stop there**: do not run the next one, and do not start the app.
Step 2 keeps a dump of the current database for exactly that case.

1. Stop the app.

       docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env stop app

2. Dump the current database first, and check the dump is not empty.

       docker exec retratar-postgres-1 pg_dump -U retratar -d retratar -Fc > pre-restore-$(date +%F).dump
       test -s pre-restore-$(date +%F).dump && echo ok

3. Recreate the database empty. The app is stopped, so nothing is connected.
   `ssl = on` is set cluster-wide with `ALTER SYSTEM`, so dropping the database
   does not undo it.

       docker exec retratar-postgres-1 psql -U retratar -d postgres -c 'DROP DATABASE retratar' -c 'CREATE DATABASE retratar'

4. Restore into the empty database. No `--clean`. `--single-transaction` and
   `--exit-on-error` make a bad dump fail as a whole: the database stays empty
   rather than half restored.

       docker exec -i retratar-postgres-1 pg_restore -U retratar -d retratar --no-owner --single-transaction --exit-on-error < db.dump

5. Bring the schema up to the version of the app you are about to start.

       docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env run --rm app -migrate

6. Empty the photos volume, then extract. Photos uploaded after the backup have
   no row in the restored database, so they must not stay behind. Both commands
   run as uid 65532, which owns the volume's root.

       docker run --rm --user 65532:65532 -v retratar_photos:/p alpine:3 find /p -mindepth 1 -delete
       docker run --rm -i --user 65532:65532 -v retratar_photos:/p alpine:3 tar xzf - -C /p < photos.tgz

7. Start the app, then check it: the Stats command above, and a page with a
   photo.

       docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env start app

8. Delete `db.dump` and `photos.tgz` on both machines. Keep
   `pre-restore-*.dump` until the site is confirmed good, then delete it too.

## Traps

Each of these has already been paid for once.

**Compose project name.** `name: retratar` in `docker-compose.yml` is load
bearing. Compose otherwise derives the project name from the compose file's
parent directory — `deploy` — which collides with any other Compose project on
the same host whose file also lives in a directory called `deploy`. Without the
explicit name, `up` treats that project's containers as orphans and creates
volumes inside its namespace.

**Postgres port.** The container publishes **5433**. The Pi's native
YunoHost-managed `postgresql@15-main` owns 5432. `DATABASE_URL` in
`deploy/retratar.env` must say 5433.

**Postgres TLS is set in the data volume, not by flags.** `config.Validate`
refuses `sslmode=disable` whenever `ENV=production`, with no loopback
exception, on purpose. Passing `-c ssl=on` via the compose `command:` does
**not** reach the running server — the `postgres:18-alpine` entrypoint re-execs
itself twice and the flags are lost. It is set with `ALTER SYSTEM SET ssl = on`
plus a restart, once per data volume; it persists in `pgdata`. Confirm with
`SELECT source FROM pg_settings WHERE name='ssl'` — `command line` means it did
not take, `configuration file` means it did.

**Architecture.** The `Dockerfile` has `ARG TARGETARCH=amd64`. An explicit
default overrides BuildKit's automatic platform-arg injection even when
`platform: linux/arm64` is set, so a build that looks correct produces an amd64
binary that dies with `exec format error` on the Pi. `build.args:
TARGETARCH: arm64` must stay set.

**Secrets.** `deploy/retratar.env` (Postgres password, SMTP credentials) and
`deploy/postgres-tls/` are Pi-local and gitignored. Keep an encrypted copy
off the host. Do not `git clean -x` this checkout.
