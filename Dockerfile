# Multi-stage: the toolchain never ships to production.
#
# The final image is distroless/static — no shell, no package manager, no libc.
# If the process is ever compromised there is nothing in the image to pivot to.

# The version must match the `toolchain` line in go.mod; CI reads it from
# there, and Dependabot proposes patch bumps.
FROM golang:1.27.2-alpine AS build

WORKDIR /src

# Copy manifests first: this layer caches until dependencies actually change.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

# CGO_ENABLED=0 produces a static binary for the distroless/static base.
# -trimpath removes local filesystem paths from the binary, which are both an
# information leak and a source of build irreproducibility.
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# The photos directory, created here so the final image owns it. A new named
# volume copies the ownership of the image directory it mounts over; without
# this one the volume would be root-owned and the nonroot process could not
# write to it.
RUN mkdir -p /out/data/photos

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /usr/local/bin/server
COPY --from=build --chown=65532:65532 /out/data /data

# distroless nonroot is uid 65532. The process writes only to /data/photos,
# which is a volume; run the container with --read-only.
USER nonroot:nonroot

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
