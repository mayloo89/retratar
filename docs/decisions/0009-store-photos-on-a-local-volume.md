# 9. Store photos on a local volume

Date: 2026-10-20
Status: accepted

## Context

Few people use the service at first, and each has one current photo. Photos are
re-encoded on upload (ADR 0008), so every file is small.

The service runs on a single host.

## Decision

We will store photos as files on a Docker volume, each named by a random key and
written atomically: a temporary file in the same directory, then a rename.

The app serves a photo on its owner's page host, with an immutable cache header.
The key changes with every upload, so a URL always names the same bytes.

The database holds the key. The filesystem only ever sees keys that passed
validation, whether they came from the database or from a URL.

We will defer object storage (the R2 in PLAN).

## Consequences

The volume must be part of the backup.

One host means one disk. A failed disk loses the photos until they are restored.

Moving to object storage later only changes `internal/photostore`. The key, the
database column and the URLs stay as they are.

A crash between saving a file and committing its key leaves an orphaned file.
That is acceptable at this scale; a sweep can come later.
