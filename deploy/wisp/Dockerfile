# The wisp server image (cmd/wisp). Built by deploy/deploy.sh on the DEV
# machine and shipped to the server as an image (docker save | ssh |
# docker load), so the server never compiles anything. Building there
# (toolchain pull, modules, the SQLite driver: ~2 GB of temporary space)
# filled bh2's shared 8.7 GB disk on 2026-10-02.
#
# The build stage runs on the build machine's own platform and
# cross-compiles for the target (Go needs no emulation, and the SQLite
# driver is pure Go, so CGO stays off); only the tiny final layer is
# per-architecture. golang:1.24 pins the toolchain, so the result
# doesn't depend on whichever Go the dev machine has installed.
#
#   docker build --platform linux/amd64 -f deploy/wisp/Dockerfile -t wisp:test .
FROM --platform=$BUILDPLATFORM golang:1.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY hook ./hook
COPY internal ./internal
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/wisp ./cmd/wisp

# distroless/static rather than scratch: SQLite wants a writable /tmp for
# large sorts, and this gives one plus CA certs, with no shell or package
# manager.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/wisp /wisp
ENTRYPOINT ["/wisp"]
CMD ["serve", "-addr", ":8080", "-data", "/data", "-registry", "/etc/wisp/products.json"]
