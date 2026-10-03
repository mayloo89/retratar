# 7. Use cross-origin protection instead of CSRF tokens

Date: 2026-09-29
Status: accepted

## Context

State-changing app routes (`POST /login`, `/login/{token}`, `/handle`, `/mood`,
`/logout`) rely on the session cookie's `SameSite=Lax`. Lax withholds the cookie
from cross-*site* requests only; any host under `retratar.com.ar` would count as
same-site. `docs/architecture.md` required real CSRF protection beyond
SameSite. The classic fix is a per-session token in every form. Go 1.25 added
`net/http.CrossOriginProtection`, which rejects non-safe requests the browser
marks as cross-origin through `Sec-Fetch-Site` (sent by all major browsers since
2023), falling back to comparing `Origin` with `Host`.

## Decision

We will wrap the app surface in `http.CrossOriginProtection` with no trusted
origins and no bypass patterns, and will not add form tokens. The check is
*origin*-strict, so a sibling subdomain is rejected too, which is stronger than
SameSite.

## Consequences

No per-form plumbing, and no dependency (ADR 0003). Requests carrying neither
`Sec-Fetch-Site` nor `Origin` are allowed; those are non-browser clients, which
CSRF does not involve. The check depends on the `Host` header reaching the app
unchanged, which nginx already guarantees (`proxy_set_header Host $host`) and
`hostSplit` already relies on. Revisit, and add tokens, if the app must support
browsers that send neither header, or if a state-changing endpoint must accept
cross-origin browser requests. The login nonce (`internal/web/nonce.go`) stays;
it protects the link-confirmation step from forged confirmations and
prefetchers, which is a different problem.
