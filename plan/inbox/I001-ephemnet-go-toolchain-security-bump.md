---
id: I001
title: "EphemNet's Go toolchain security bump (1.24 -> 1.26.9) — wisp's own go.mod is on an even older, equally EOL version"
status: DONE
from_project: EphemNet
to: wisp maintainers
created: 2026-10-22
updated: 2026-10-22
---

## Message

After a user report of a recent Go CVE, EphemNet checked its own Go
toolchain status and found it had been running `go.mod`'s `go 1.24.0`
(and `golang:1.24` in its Dockerfiles) unpatched since **2026-02-11**
— the day Go 1.26 shipped and Go stopped backporting security fixes to
1.24 (Go only patches the latest two major branches; Go 1.27 has since
shipped too, 2026-08-19).

Checked `wisp`'s own `go.mod` and `deploy/wisp/Dockerfile` from here —
same exposure, actually a step further behind: `go 1.23` in `go.mod`,
`golang:1.24` in the Dockerfile. Same missed fixes apply:

- `CVE-2026-94447` — Go toolchain checksum-bypass (malicious module
  proxy could serve a bad toolchain).
- `CVE-2026-97031` (`crypto/tls` ECH handling), `CVE-2026-97032`
  (HTTP/2 server crash in `net/http`) — fixed in `go1.26.9`/`go1.27.2`,
  released 2026-10-08.
- From the August round: `CVE-2026-56862` (`crypto/tls` KeyUpdate DoS),
  `CVE-2026-56853` (`net/http` HTTP/2 preface timeout bypass),
  `CVE-2026-56858` (`html/template` XSS).

EphemNet's own fix (bump `go.mod` to `go 1.26.9`, repin all Dockerfiles
to `golang:1.26`, bump `golang.org/x/crypto` to v0.58.0, verify with a
real build/test/docker-build/deploy cycle) is written up in full in
EphemNet's own `plan/actions/A022-go-toolchain-security-bump-1-26.md`.

Not fixing `wisp` from here — this is a heads-up, not a drive-by patch
to a sibling repo. (This is also `wisp`'s first `plan/inbox/` message
— that directory didn't exist here before; created it to use the same
cross-repo notification convention `cinder`/`offgrid` already have.)

## Log

2026-10-22 — Sent, right after EphemNet's own bump shipped to dev and
live and A022 was filed.

2026-10-22 — Read by wisp; acted on in A002, which targets go1.26.10.
1.26.9 already has GO-2026-6809 after it, and health-wisp/uptime-wisp
were found built with go1.23.4 from PATH.
