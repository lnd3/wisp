# Runtime-only image for the wisp server (cmd/wisp). The static binary is
# cross-compiled by deploy/deploy.sh on the dev machine, from the same
# committed snapshot it ships, and copied in here, so the server never
# runs a Go toolchain, downloads modules or holds a build cache. On bh2
# that build needed ~2 GB of temporary space and filled the shared
# 8.7 GB disk on 2026-10-02. Build context: the repo root (see
# docker-compose.yml).
#
# distroless/static rather than scratch: SQLite wants a writable /tmp for
# large sorts, and this gives one plus CA certs, with no shell or package
# manager.
FROM gcr.io/distroless/static-debian12
COPY deploy/wisp/wisp /wisp
ENTRYPOINT ["/wisp"]
CMD ["serve", "-addr", ":8080", "-data", "/data", "-registry", "/etc/wisp/products.json"]
