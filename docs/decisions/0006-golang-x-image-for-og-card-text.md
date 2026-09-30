# 6. Use golang.org/x/image to draw text on the OG card

Date: 2026-09-15
Status: accepted

## Context

`GET /og.png` renders the handle, mood label and note onto the mood's colour
as a PNG, for link previews in WhatsApp, Telegram, Discord, iMessage,
Twitter and Slack. Those consumers require a raster image; SVG does not
render as a preview in any of them.

The standard library's `image`, `image/draw` and `image/png` can allocate a
bitmap, fill it with the mood colour, and encode it to PNG. None of them can
draw text: there is no glyph rasteriser in the standard library. Decision 3
commits this project to the standard library and to adding dependencies
reluctantly, with the reason recorded in the pull request each time.

`golang.org/x/image` provides `font`, `font/opentype`, `font/sfnt` and
`math/fixed`, which together parse a TTF and draw a string onto an
`image.RGBA` at a fixed size. It also bundles the Go font family
(`font/gofont/goregular`, `font/gofont/gobold`) as embedded Go source, so no
separate font file needs to be vendored or downloaded at build time. The Go
fonts and the rest of the `golang.org/x/image` module ship under the same
BSD-3-Clause licence as Go itself (module `LICENSE`, "Copyright 2009 The Go
Authors").

`/og.png` is on the pages surface, unauthenticated, and does not touch a
session, a credential or a database write path beyond the read-only lookups
`handlePage` already does. It is the same trust boundary as any other GET
handler in `internal/web`.

## Decision

We will add `golang.org/x/image` as a direct dependency, used only by a new
`internal/ogcard` package. We will use the bundled Go Regular and Go Bold
faces from `font/gofont` rather than shipping our own font file, so there is
no separate asset to license, vet or update.

## Consequences

One more dependency on the request path, but it is `golang.org/x/image`
itself (Google-maintained, BSD-3-Clause, no transitive dependencies of its
own beyond the rest of `golang.org/x`), and it is exercised only by the
unauthenticated pages surface's `/og.png` route — never by anything that
handles a session or a credential.

Swapping the typeface later means changing which `gofont` subpackage
`internal/ogcard` imports, or embedding a different TTF and passing it to
`opentype.Parse`; either is a one-file change.
