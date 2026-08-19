# retratar

**Tu página, ahora.**

A personal homepage that shows your present: one photo in a frame, one mood that
tints the page, and whatever else you want beside them. Old-web aesthetic, no
code required, a URL you can drop anywhere.

Every other profile page you own looks the same for years. This one is different
every visit.

```
retrat.ar          →  public pages, one subdomain per person: sebas.retrat.ar
retratar.com.ar    →  the app: sign in, edit your page
```

There is no feed, no follower count, no discovery, no algorithm. A page is worth
having when nobody else is here.

## Status

Early development. Not open for sign-ups.

## Roadmap

Named phases, no dates — this is a one-person project.

- [ ] **Foundation** — hosting, sign-in, page rendering
- [ ] **Photo & mood** — the core: a framed photo, a mood that colours the page
- [ ] **Pages** — block editor and themes
- [ ] **Signals** — guestbook, visit counts, weekly digest
- [ ] **Private beta**
- [ ] **Buzz** — a wordless ping between two people

## Development

Requires Go 1.26 or newer.

```sh
cp .env.example .env
git config core.hooksPath .githooks   # gofmt + vet before each commit
make run                              # http://app.localhost:8080
make check                            # tidy, format, lint, test, vulnerability scan
```

`make help` lists every target. Diagnostics — pprof, expvar, `/version` — are on
`http://127.0.0.1:8081`, a separate listener that refuses to bind anywhere but
loopback.

Use `localhost` hostnames locally. The session cookie is always `Secure`, and
browsers treat `localhost` as a secure context — an IP address or a custom
hostname over plain HTTP will silently drop the cookie.

### Layout

Packages are organised by feature, not by layer. There is no `models`,
`services`, or `repositories` package, and no `pkg/`.

```
cmd/server/         process entry point and wiring
internal/buildinfo/ what this binary was built from
internal/config/    environment configuration and its invariants
internal/web/       routing, middleware, security headers
docs/decisions/     architecture decision records
```

Dependencies are struct fields assigned in `main`. There is no DI container, and
`main` takes its environment as a parameter so the startup path is testable.

Tool versions are pinned: `govulncheck` through the `tool` directive in
`go.mod`, `golangci-lint` through a version in the Makefile and CI.

## Architecture

One binary serves both surfaces, split by the request's `Host` header. See
[docs/architecture.md](docs/architecture.md), and
[docs/decisions/](docs/decisions/) for why each choice was made.

The app and the pages live on **different registrable domains**, and that is
load-bearing rather than cosmetic: pages render content their owners control, so
they must sit outside the origin that issues the session cookie. The server
refuses to start if the two hosts share a registrable domain, and the tests in
`internal/config` and `internal/web` exist to keep it that way.

## Contributing

Not accepting contributions yet. Bug reports and security reports are welcome —
see [SECURITY.md](SECURITY.md) for anything security-related.

## Licence

All rights reserved — see [LICENSE](LICENSE). The source is published to be read
and reviewed, not reused. Running it as a service is not permitted. Ask if you
want to do something with it.
