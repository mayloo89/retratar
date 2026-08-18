# 3. Use the standard library for HTTP, and add dependencies reluctantly

Date: 2026-08-18
Status: accepted

## Context

Go 1.22 gave `net/http.ServeMux` method and wildcard patterns, which removes the
original reason most projects reached for chi, gorilla or echo. Every dependency
in a web server is code running on the request path with the ability to read
sessions and uploads, and this project is maintained by one person who cannot
audit a large tree.

## Decision

We will use `net/http`, `log/slog`, `html/template` and `database/sql` from the
standard library. Middleware is a `func(http.Handler) http.Handler`, composed
explicitly.

We will add a dependency when it does something the standard library does not,
and record the reason in the pull request. Expected additions: `templ` for typed
templates, `sqlc` for typed queries, `goose` for migrations, a Postgres driver.

We will not add an ORM or a dependency-injection framework. Dependencies are
struct fields assigned in `main`.

## Consequences

More code written by hand: routing helpers, CSRF, sessions, rate limiting. Each
of those is a place to get security wrong, and each needs its own tests.

In exchange the dependency graph stays small enough to actually read, upgrades
are rare, and the request path holds no third-party code.
