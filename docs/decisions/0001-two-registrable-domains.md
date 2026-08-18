# 1. Serve pages and the app from two registrable domains

Date: 2026-08-18
Status: accepted

## Context

The product renders pages whose content and styling their owners control, and it
also runs an app that issues session cookies. Both are served by one binary.

Cookies do not follow the same-origin policy. Any subdomain can write a cookie
scoped to its parent domain, and `SameSite` treats subdomains as same-site. If
user pages lived under the app's domain, a page could read a `Domain`-scoped
session cookie, or set one on the parent and fix the owner's session — cookie
tossing. `SameSite`-based CSRF protection would also stop protecting anything.

`com.ar` is on the Public Suffix List, so `retratar.com.ar` and `retrat.ar` are
separate registrable domains and browsers enforce the boundary for us. This is
the same shape as `github.com` and `githubusercontent.com`.

## Decision

We will serve the app from `retratar.com.ar` and pages from `*.retrat.ar`.

`config.Config.Validate` refuses to start the process if the two hosts share a
registrable domain. The session cookie is named `__Host-session`, so the browser
drops it unless it is `Secure`, `Path=/`, and carries no `Domain`.

## Consequences

Two domains to buy, renew and hold certificates for. Cross-surface links are
absolute rather than relative. Local development needs two hostnames.

In exchange, the most severe class of bug in this product — a user page reaching
the session — becomes structurally impossible rather than a sanitiser's
responsibility. Reversing this later would mean re-auditing every rendering path.
