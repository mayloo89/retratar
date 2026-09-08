# Multi-stage: the toolchain never ships to production.
#
# The final image is distroless/static — no shell, no package manager, no libc.
# If the process is ever compromised there is nothing in the image to pivot to.

FROM golang:1.27-alpine AS build

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

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /usr/local/bin/server

# distroless nonroot is uid 65532. The process never needs to write to its
# filesystem; run the container with --read-only.
USER nonroot:nonroot

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
