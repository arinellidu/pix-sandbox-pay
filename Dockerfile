# --- build ------------------------------------------------------------------
# Pinned to the *build* platform, not the target: a multi-arch build otherwise
# runs this whole stage under QEMU for every foreign architecture, and
# emulating a Go compiler costs ten minutes to produce a binary that
# cross-compiling produces in seconds. The output is static and CGO-free, so
# there is nothing about the target that has to be present to build for it.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

ARG VERSION=dev
# Supplied by buildx, once per target platform.
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/pix-sandbox ./cmd/pix-sandbox

# Distroless has no shell, so the data directory is staged here.
RUN mkdir -p /out/data

# --- runtime ----------------------------------------------------------------
# Distroless static: no shell, no package manager, nonroot by default.
# modernc.org/sqlite is pure Go, so nothing here needs libc.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/pix-sandbox /pix-sandbox
# Owned by nonroot (65532) so the sandbox can create its database at runtime.
COPY --from=build --chown=65532:65532 /out/data /data

ENV PIX_SANDBOX_ADDR=:8080 \
    PIX_SANDBOX_DB=/data/sandbox.db

EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot

ENTRYPOINT ["/pix-sandbox"]
