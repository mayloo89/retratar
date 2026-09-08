# Architecture

## Two surfaces, one binary

```
                    Cloudflare
                        |
              nginx on the Pi (YunoHost)
                        |
        +---------------+----------------+
        |                                |
  retratar.com.ar                  *.retrat.ar
  sign-in, editor                  rendered pages
        |                                |
        +----------- Go binary ----------+
                        |
                Postgres  +  object storage
```

The split happens in `internal/web.Server.hostSplit`. A request whose `Host`
matches neither surface gets a 404 — never a default. A hostname pointed at this
server that we did not configure is not ours, and serving the sign-in form on it
would put credentials on a foreign origin.

## Why two domains

`retratar.com.ar` issues the session cookie. `retrat.ar` renders pages whose
content and styling their owners control. Those two jobs cannot share an origin.

If pages lived under the app's domain, a page could:

- read the session cookie, if the cookie were `Domain`-scoped to the parent;
- write a `Domain`-scoped cookie on the parent and fix the owner's session
  (*cookie tossing*), because cookies ignore the same-origin policy and any
  subdomain can write to its parent;
- defeat `SameSite`, which treats subdomains as same-site, so CSRF protections
  that lean on it stop protecting anything.

`com.ar` is on the [Public Suffix List](https://publicsuffix.org/), so
`retratar.com.ar` and `retrat.ar` are separate registrable domains. Browsers
enforce the boundary for us.

Three things keep it enforced:

1. `config.Config.Validate` refuses to start the process if the two hosts share
   a registrable domain.
2. The session cookie is named `__Host-session`. The prefix is enforced by the
   browser: the cookie is dropped unless it is `Secure`, has `Path=/`, and
   carries **no** `Domain` attribute. No `Domain` means host-only.
3. `TestPagesSurfaceNeverSetsCookies` asserts that no response from a page host
   carries `Set-Cookie`.

`SameSite` is defence in depth here, not the defence. Real CSRF tokens are
required on every state-changing request.

## Why user pages have no JavaScript

The page CSP omits `script-src` entirely, so `default-src 'none'` denies every
script: inline handlers, a `<script>` that survived sanitising, `javascript:`
URLs.

This costs less than it sounds. MySpace customisation was overwhelmingly CSS and
`<marquee>`. Customisation here is CSS, and CSS alone.

`style-src` is `'self'` with no `'unsafe-inline'`. That is a constraint on the
renderer: theme CSS and per-page customisation must be served as stylesheets
from our own origin. Inlining a `style` attribute into page HTML breaks the
page — which is the intended feedback, not a bug. Do not relax it.

## Blocks are semantic, themes are one CSS file

A page is a list of semantic blocks with a fixed class contract
(`.blk-photo`, `.blk-mood`, `.blk-links`, …). A theme is one stylesheet against
that contract, so a new theme is CSS with no Go changes.

Get this wrong early and every new theme becomes a code change, and themes stop
shipping.

## Handles are DNS labels

A handle becomes a hostname, so `web.ValidHandle` is stricter than DNS: lowercase
ASCII, digits, single inner hyphens, 30 characters, no reserved names.

Unicode is excluded deliberately. A homograph handle lets one person impersonate
another's hostname, and the URL is the whole identity in this product.
