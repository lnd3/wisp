---
id: A002
title: Go toolchain security bump — build everything with go1.26.10
status: IN_PROGRESS
project: P001
created: 2026-10-22
updated: 2026-10-22
---

## Why

Inbox I001 (from EphemNet): wisp's `go.mod` said `go 1.23` and
`deploy/wisp/Dockerfile` built with `golang:1.24`. Go only patches its
two newest major versions, so both had missed every Go security fix
since 1.26 shipped (2026-02-11), including the October round
(`crypto/tls` ECH, `net/http` HTTP/2 server crash) and August's
`html/template` XSS.

It was one step worse than I001 saw: health-wisp and uptime-wisp
aren't built in a container. Their deploy scripts built with whatever
`go` came first on the dev machine's PATH, which was `go1.23.4`. So all
three health-wisp hosts and uptime-wisp ran Go 1.23.4 binaries.

The exposure was moderate. On bh2/bh3, Caddy terminates TLS and
HTTP/2 in front of wisp and health-wisp. `html/template` matters for
wisp's dashboard and uptime-wisp's status page.

## What

- **`go.mod`:** `go 1.25.0` plus `toolchain go1.26.10`.
  - The `go` line is a minimum language version, and products inherit
    it: anything that imports `hook` must declare at least wisp's. So
    it stays at the lowest version the requirements allow (`x/sys`
    v0.44.0 needs 1.25.0), which equals offgrid's, the oldest product.
    No product is forced to bump.
  - The `toolchain` line only applies when wisp itself is the main
    module.
  - go1.26.9 (EphemNet's choice) was already one patch behind:
    govulncheck flagged GO-2026-6809 (crypto/x509), fixed in 1.26.10.
- **`golang.org/x/sys`:** v0.22.0 → v0.44.0, for GO-2026-5024
  (Windows-only; never built here, bumped anyway).
- **`deploy/wisp/Dockerfile`:** `golang:1.26.10`, pinned to the patch.
- **`deploy/health/deploy.sh`, `deploy/uptime/deploy.sh`:** both export
  `GOTOOLCHAIN=local`, so the go command never downloads a toolchain
  itself. They then check the built binary's Go version against
  go.mod's toolchain line and refuse an older one. Tested: 1.23.4 is
  refused by go.mod's minimum, 1.26.9 by the guard, and 1.26.10 is
  accepted.
- **Dev machine:** go1.26.10 installed in `~/sdk/go1.26.10` via
  `golang.org/dl`. That download is checksum-verified, unlike letting
  the old 1.23.4 auto-switch, which is the path CVE-2026-94447 is
  about.
- **Checks:** govulncheck finds no vulnerabilities, and
  `go test -race ./...` passes.

## Deploy

Rebuild and redeploy everything:
- wisp: bh2 live.
- health-wisp: bh2, bh3, rbserver1.
- uptime-wisp: rbserver1.

## Log

2026-10-22 — Filed from I001 with the user's go-ahead; code changes
made and tested.
