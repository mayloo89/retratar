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

**3. Verify the rate limiter is actually live.** Check it rather than
assuming:

    for i in $(seq 1 8); do
      curl -s -o /dev/null -w "%{http_code} " \
        -X POST -d 'email=rl-probe@example.com' http://127.0.0.1:8082/login
    done; echo

Expect five `200` then `429` — the bucket is 5 burst, one token back every
20s. All eight returning `200` means the container is still the old image; go
back to step 2. Use a throwaway address: every non-429 sends a real email.

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
