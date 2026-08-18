# 2. User pages carry no JavaScript

Date: 2026-08-18
Status: accepted

## Context

Customisation is the point of the product, and the obvious way to offer it is to
let people paste HTML, as MySpace did. Every service that allowed script on
user-controlled pages has spent years fighting stored XSS: sanitisers are
allowlists that rot, and the usual bug is not a weak sanitiser but a row written
before the sanitiser improved.

MySpace customisation was overwhelmingly CSS and `<marquee>`. The nostalgic
feeling people want comes from layout, colour and motion, not from scripting.

## Decision

We will omit `script-src` from the page Content-Security-Policy entirely, so
`default-src 'none'` denies every script: inline handlers, a `<script>` that
survived sanitising, and `javascript:` URLs.

`style-src` is `'self'` with no `'unsafe-inline'`. Theme CSS and per-page
customisation are served as stylesheets from our own origin.

We will sanitise on write *and* on render, because the sanitiser will improve
after rows are already stored.

## Consequences

Some effects are impossible without script, and a few users will ask. Inlining a
`style` attribute breaks the page under this policy, which constrains the
renderer permanently — that is intended, not a defect.

CSS still needs its own filtering: `position: fixed` overlays, `@import`, and
arbitrary `url()` are all abusable without a line of JavaScript.
