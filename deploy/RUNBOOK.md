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

Last verified 2026-09-15.

| surface | state |
|---|---|
| `retratar.com.ar` | serving, HTTP 200 |
| `retrat.ar`, `*.retrat.ar` | **down** — TCP connects, TLS completes, the HTTP request is reset |

Both served 200 on 2026-09-08, so this is a regression, not a setup that never
worked.

**The running deploy predates PR #10.** The Pi was cloned from
`feat/postmark-smtp-and-pi-deploy` while that branch was unmerged and has not
been re-deployed since. The container and the installed nginx configs are both
older than `develop`.

The consequence is a live security gap, not a cosmetic drift: **neither layer
of the rate limiting is on the box.** `POST /login` on the live host mints a
magic link and hands it to the SMTP relay for any caller, unthrottled — an
email flood aimed at someone else's inbox and a bill on the relay. Procedure A
is the fix. Treat it as the reason to open the laptop, not as tidying.

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

## Procedure B — diagnose the `retrat.ar` reset

What is already ruled out, from off-network, so you do not repeat it:

- **Not DNS.** Both zones are on Cloudflare nameservers.
- **Not certificates.** The edge serves a valid cert with SANs `retrat.ar` and
  `*.retrat.ar`; the handshake completes and verifies.
- **Not the network path or an edge IP.** `retratar.com.ar` succeeds from both
  Cloudflare edge IPs and `retrat.ar` fails from both.
- **Not the Go app.** The app answers `Host: retrat.ar` with a redirect and an
  unclaimed handle with 404. It has no code path that resets a connection.

That leaves nginx on the Pi and the Cloudflare `retrat.ar` zone. Work outward:

    sudo nginx -t
    sudo nginx -T 2>&1 | grep -nE 'server_name|certs/retrat|retratar_client_ip'
    ls -l /etc/nginx/certs/retrat.ar/
    docker ps --filter name=retratar
    sudo ss -ltnp | grep -E ':(443|8082)'

Then bypass each layer in turn, innermost first:

    # app directly — expect a redirect, and 404 for an unclaimed handle
    curl -sS -i -H 'Host: retrat.ar'       http://127.0.0.1:8082/ | head -1
    curl -sS -i -H 'Host: sebas.retrat.ar' http://127.0.0.1:8082/ | head -1

    # nginx directly, skipping Cloudflare (-k: the Origin CA cert is not
    # publicly trusted, which is expected and not the bug)
    curl -sSk -i -H 'Host: sebas.retrat.ar' https://127.0.0.1/ | head -1

If the app answers and nginx does not, it is the server block or the cert path.
If nginx answers and the public hostname still resets, it is the Cloudflare
zone — compare `retrat.ar` against the working `retratar.com.ar` zone field by
field: zone status, proxied A records for **both** the apex and the `*`
wildcard, SSL/TLS mode, and any Rules. The wildcard is a separate record from
the apex and is easy to miss; `sebas.retrat.ar` needs it.

The likeliest single cause, given nothing was deliberately changed: nginx has
no active `:443` server block matching `retrat.ar`, so SNI falls through to
YunoHost's catch-all, which closes the connection without responding — which
is exactly a reset after a completed handshake.

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
