# 8. Re-encode uploaded photos

Date: 2026-10-13
Status: accepted

## Context

Phone photos carry EXIF metadata, including the GPS coordinates of where they
were taken. Publishing one as uploaded would publish that location.

Uploaded files are untrusted. An image's header can declare dimensions far
beyond its file size, so a small file can make a decoder allocate gigabytes.

The server is a small single-board machine with little memory to spare.

## Decision

We will never store or serve an uploaded file as uploaded.

Every photo is decoded and re-encoded as a JPEG of at most 1200px on its long
side, with no metadata. The orientation is read first and applied to the
pixels, so the result displays upright without the tag.

Dimensions are checked from the header before any pixel is decoded, and only
JPEG and PNG are accepted. The format is named explicitly rather than taken
from whatever decoders happen to be registered.

Scaling uses `golang.org/x/image/draw`, which is already a dependency
(ADR 0006). No new module is added. The code lives in `internal/photo` as pure
functions: no HTTP, no database, no disk.

## Consequences

Location and camera data never leave the upload request.

The stored size is predictable.

The original quality is lost, which is acceptable for a page photo.

HEIC is not accepted. iOS converts to JPEG when a browser uploads a photo; this
is to be verified on a device when the upload is wired in.

Processing costs CPU for every upload.
