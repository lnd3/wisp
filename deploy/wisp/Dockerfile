# Builds the wisp server (cmd/wisp) as a static binary — build from the
# repo root:
#   docker build -f deploy/wisp/Dockerfile -t wisp .
#
# golang:1.24 to match the sibling repos' images on bh2; go.mod's own
# `go 1.23` is just the minimum. CGO_ENABLED=0 works because the SQLite
# driver (modernc.org/sqlite) is pure Go.
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY hook ./hook
COPY internal ./internal
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/wisp ./cmd/wisp

# distroless/static rather than scratch: SQLite wants a writable /tmp
# for large sorts, and this gives one plus CA certs, at no extra attack
# surface worth mentioning (no shell, no package manager).
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/wisp /wisp
ENTRYPOINT ["/wisp"]
CMD ["serve", "-addr", ":8080", "-data", "/data", "-registry", "/etc/wisp/products.json"]
