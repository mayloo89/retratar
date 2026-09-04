# 5. Run Postgres and the app in Docker, not systemd + a native role

Date: 2026-09-04
Status: accepted

## Context

`deploy/retratar.service` and the original provisioning plan assumed a native
Postgres role created with `sudo -u postgres psql`, and the Go binary running
as a systemd unit under a dedicated system user.

The Pi's YunoHost install restricts `sudo` to a specific command whitelist per
user — `sudo -u postgres psql` is not in it, for any command, so creating the
`retratar` role and database this way is not possible over SSH without asking
for a sudoers change. `docker`, by contrast, needs no `sudo` at all: the
deploying user is already in the `docker` group, which is also how
`circl.unlug.ar` already runs on this same Pi — its own Postgres is a
container, entirely separate from the native `postgresql@15-main` service.

## Decision

We will run retratar's Postgres and the app itself as two services in
`deploy/docker-compose.yml`, built from the repository's existing
`Dockerfile` (already multi-stage, distroless, and `TARGETARCH`-aware — built
for this without changes).

The app service uses `network_mode: host` rather than a bridge network.
`config.Config.Validate` refuses `sslmode=disable` outside of a literal
loopback connection in production, and that check stays as strict as it is
for `internal/config`'s local dev Postgres — a container-to-container bridge
hop would need its own TLS story to satisfy it for no real security gain on a
single host. Host networking makes `DATABASE_URL`'s host `127.0.0.1` for
real, and it means nginx's `proxy_pass http://127.0.0.1:8082` (see
`deploy/nginx/*.conf`, decision 0004) needs no change — the app binds that
address directly, same as a bare binary would have. Postgres keeps normal
bridge networking, published only to `127.0.0.1:5432`, matching
`compose.yaml`'s existing dev shape.

## Consequences

`deploy/retratar.service` is dead on this deploy path, same status as
`deploy/Caddyfile` after decision 0004 — kept as the reference for a future
dedicated-VPS move, not deleted.

The app container needs `network_mode: host`, which is Linux-only; this was
never a portability concern since the Pi is the only target.

Secrets (`POSTGRES_PASSWORD`, the SMTP credentials, `DATABASE_URL`) live in
`deploy/retratar.env` on the Pi, gitignored, never committed — same rule as
the systemd plan's `/etc/retratar/env`, just read by `docker compose`'s
`env_file` instead of `EnvironmentFile=`.
