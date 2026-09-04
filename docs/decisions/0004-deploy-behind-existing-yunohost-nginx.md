# 4. Deploy behind the Pi's existing nginx, fronted by Cloudflare

Date: 2026-08-29
Status: accepted

## Context

`deploy/Caddyfile` was written assuming a dedicated VPS where Caddy owns ports
80 and 443 and issues certificates itself — on-demand per hostname for
`*.retrat.ar`, normally for `retratar.com.ar`.

The actual first deploy target is a Raspberry Pi that already runs YunoHost
for several other services (Nextcloud, Pixelfed, XMPP, a hand-rolled app on
`circl.unlug.ar`). YunoHost's own nginx already owns 80 and 443 and manages
certificates through Let's Encrypt for each domain it's told about. Caddy
cannot bind those ports too, so it cannot run there as designed.

Both `retrat.ar` and `retratar.com.ar` are registered at NIC.ar but, unlike the
Pi's other domains, had no DNS delegation yet. The user's other domains on
this Pi (`unlug.ar`, `circl.ar`) are both delegated to Cloudflare, and
`circl.ar` is served proxied through it. `PLAN.md`'s own architecture section
already wanted Cloudflare in front for CDN and CSAM scanning, so putting these
two domains on Cloudflare too — proxied — is not a new dependency, it's using
one already in play.

Proxying through Cloudflare also solves the wildcard problem for `*.retrat.ar`
immediately instead of deferring it: NIC.ar has no YunoHost-supported DNS
provider module, which rules out a DNS-01 challenge for a wildcard Let's
Encrypt cert. Cloudflare's Universal SSL covers `*.retrat.ar` at the edge with
no DNS-01 step at all.

## Decision

We will front the app with the Pi's existing nginx instead of Caddy, and put
`retrat.ar` / `retratar.com.ar` on Cloudflare, proxied (orange cloud), SSL/TLS
mode Full (strict).

nginx (`deploy/nginx/*.conf`) terminates TLS with a **Cloudflare Origin CA**
certificate — not a publicly-trusted Let's Encrypt one, and not something
YunoHost's `domain add`/cert flow manages. Full (strict) mode is what makes
Cloudflare check that cert; browsers never see it, since browsers only ever
talk to Cloudflare's edge. Each cert is generated once in the Cloudflare
dashboard (15-year validity) and copied to the Pi by hand — there is no
renewal automation for it, because there is nothing to renew for over a
decade.

`retrat.ar`'s conf covers the domain and `*.retrat.ar` in one nginx server
block, matching the single Cloudflare Origin CA cert issued for both. Adding a
handle later needs neither a new nginx file nor a new cert — only a DNS record
if a handle ever needs its own hostname outside the wildcard, which Phase 0/1
does not.

Mail (Brevo, see the SMTP config in `internal/config`) is unrelated to this
decision but shares its DNS work: `retratar.com.ar`'s SPF/DKIM/DMARC records
also live in this same Cloudflare zone, added once alongside the A records.

## Consequences

`deploy/Caddyfile` and the `ask`-based on-demand TLS flow in
`internal/web/router.go` (`handleTLSCheck`) are dead code on this deploy path.
They are kept, unmodified, as the reference for a possible future move to a
dedicated VPS — not deleted, since deleting them would need re-deriving the
on-demand-TLS design later if that move happens.

The origin is reachable directly at the Pi's public IP by anyone who knows it,
bypassing Cloudflare's proxy and CSAM/WAF layer entirely — Cloudflare proxying
hides the origin from casual discovery but does not firewall it. Restricting
inbound 80/443 to Cloudflare's published IP ranges (nginx `allow`/`deny`, or a
host firewall rule) closes that gap and is a reasonable follow-up once the
first deploy is proven, not a blocker for it.

The Origin CA cert is generated and installed by hand, not automated. Losing
it or the Pi's disk means generating a new one from Cloudflare and re-copying
it — a manual, five-minute recovery, not an unattended outage risk the way a
missed Let's Encrypt renewal would be.
