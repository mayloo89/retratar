# Deploy runbook

Operational companion to the setup instructions, which live in the header
comments of the files they belong to and are not repeated here:

- `deploy/docker-compose.yml` — first-time Postgres + app bring-up, TLS
  enablement, migrations
- `deploy/nginx/retrat.ar.conf` and `retratar.com.ar.conf` — Cloudflare zone
  setup, Origin CA certificates, nginx install
- `docs/decisions/0004-*` and `0005-*` — why the topology is what it is

This file covers the things you only learn the second time: how to re-deploy,
how to diagnose the surface that is currently down, and the traps that have
already cost an afternoon each.

## The box

`ssh retratar-pi` → `192.168.0.87`. **LAN-only.** Nothing here can be done from
off-network; there is no public SSH and no VPN. The checkout lives at
`~/retratar` and the app runs from it via Docker Compose.

## Current state

Last verified 2026-09-24.

| surface | state |
|---|---|
| `retratar.com.ar` | serving, HTTP 200 |
| `sebas.retrat.ar` | serving, HTTP 200, body `page: sebas` |

Both surfaces are up. An earlier revision of this file recorded `*.retrat.ar`
as down; that was a local TLS filter, not the box. See *Verifying site state*
below before you trust any probe run from a laptop.

**The running deploy predates PR #8.** The Pi was cloned from
`feat/postmark-smtp-and-pi-deploy` while that branch was unmerged and has not
been re-deployed since. The live body dates it exactly: `page: sebas` is the
scaffold stub from `internal/web/router.go` in `3ba9b06`, which PR #8 replaced
with the real `html/template` render.

So the box is running roughly three weeks and seven merged PRs behind
`develop`, and the drift is not cosmetic:

- **No rate limiting**, neither layer. `POST /login` on the live host mints a
  magic link and hands it to the SMTP relay for any caller, unthrottled — an
  email flood aimed at someone else's inbox, and a bill on the relay. This is
  a live security gap.
- **No page render.** No mood, no note, no `theme.css`. The product's visible
  half is a plain-text handle echo.

Procedure A is the fix. Treat it as the reason to open the laptop, not as
tidying.

## Procedure A — re-deploy from `develop`

Do this first, and in this order: nginx before the app, because a broken nginx
config is the failure that leaves you with nothing serving at all.

    ssh retratar-pi
    cd ~/retratar
    git fetch origin
    git checkout develop && git pull --ff-only

The branch the Pi was originally deployed from no longer exists on origin; it
was squash-merged and pruned. A stale local copy of it is harmless.

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
      run --rm app -migrate
    docker compose -f deploy/docker-compose.yml --env-file deploy/retratar.env \
      up -d --build app

`--build` matters — without it Compose reuses the old image and the deploy
silently does nothing.

**3. Verify the rate limiter is actually live.** This is the point of the
exercise, so check it rather than assuming:

    for i in $(seq 1 8); do
      curl -s -o /dev/null -w "%{http_code} " \
        -X POST -d 'email=rl-probe@example.com' http://127.0.0.1:8082/login
    done; echo

Expect five `200` then `429` — the bucket is 5 burst, one token back every
20s. All eight returning `200` means the container is still the old image; go
back to step 2. Use a throwaway address: every non-429 sends a real email.

This hits the app on loopback, so it tests the Go limiter only. The nginx
`limit_req` layer is a separate check — make the same requests through the
public hostname once the surface is back up.

## Verifying site state — read this before believing a probe

A probe run from a laptop measures the laptop's network as much as the box. On
2026-09-15 `retrat.ar` and `*.retrat.ar` appeared dead from the dev machine —
TCP connected, TLS completed, the HTTP request was reset — and that got
recorded here as an origin regression. It was a FortiGuard web filter on that
network intercepting these domains. The site was fine the whole time.

The reset-after-handshake symptom is identical either way. What distinguishes
them is the certificate issuer, so check it first, every time:

    openssl s_client -connect sebas.retrat.ar:443 -servername sebas.retrat.ar \
      </dev/null 2>/dev/null | openssl x509 -noout -subject -issuer

Cloudflare in the issuer means you are talking to the real edge. Anything else
— `O=Fortinet`, a corporate CA, any name you do not recognise — means a
middlebox is answering and **no result from that network says anything about
the box.** Retest from cellular or an external checker.

Two earlier readings of this project's state were wrong for this same reason:
a stale resolver returning registrar-parking records, and this filter. Both
times the local environment was the fault and the box was fine. Confirm the
path before concluding anything about the origin.

## If the site really is down

Work outward, innermost first:

    sudo nginx -t
    sudo nginx -T 2>&1 | grep -nE 'server_name|certs/retrat|retratar_client_ip'
    ls -l /etc/nginx/certs/retrat.ar/
    docker ps --filter name=retratar
    sudo ss -ltnp | grep -E ':(443|8082)'

    # app directly — expect a redirect, and 404 for an unclaimed handle
    curl -sS -i -H 'Host: retrat.ar'       http://127.0.0.1:8082/ | head -1
    curl -sS -i -H 'Host: sebas.retrat.ar' http://127.0.0.1:8082/ | head -1

    # nginx directly, skipping Cloudflare (-k: the Origin CA cert is not
    # publicly trusted, which is expected and not a bug)
    curl -sSk -i -H 'Host: sebas.retrat.ar' https://127.0.0.1/ | head -1

App answers and nginx does not: the server block or the cert path. Nginx
answers and the public hostname does not: the Cloudflare zone — compare it
against the working `retratar.com.ar` zone field by field, including the
wildcard A record, which is separate from the apex and is what `sebas` needs.

## Traps

Each of these has already been paid for once.

**Compose project name.** `name: retratar` in `docker-compose.yml` is load
bearing. Compose otherwise derives the project name from the compose file's
parent directory — `deploy` — which collides with the other Docker stack on
this Pi whose compose file is also in a directory called `deploy`. Without the
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
`deploy/postgres-tls/` are Pi-local and gitignored. They are not in the repo
and not backed up anywhere. Do not `git clean -x` this checkout.
