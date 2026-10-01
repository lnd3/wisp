# wisp

Cookieless, ephemeral web analytics — see `README.md` and
`plan/theses/T001-*.md` for the full picture. The one non-negotiable
constraint governing every design decision here: **nothing in this
project may require a cookie-consent popup.** Not "minimize tracking,"
not "make the banner smaller" — no mechanism that legally triggers one
is in scope at all, full stop. See `plan/designs/D001-*.md` for what
that rules in and out concretely.

A second rule, just as firm: **no visitor's browser is ever made to
contact wisp, or any other third party, for analytics**
(`plan/concepts/C001-*.md`). Forcing visitors to make requests to a
third-party site without their consent is considered simply
offensive. So products collect on their own servers, through wisp's
in-product `hook`, and forward to wisp server-to-server
(`plan/designs/D002-*.md`).

## Validate before committing

```bash
./deps/lplan/bin/plan validate ./plan
./deps/lplan/bin/plan generate-index ./plan
```

## The core discipline this project runs on

- **No browser-callable ingest, ever.** No client snippet, no
  tracking pixel, no beacon endpoint, no CORS allowance for product
  origins, no "first-party proxy script" workaround. If a metric can
  only be observed in the visitor's browser, it's out of scope (C001).
- **No raw IP persists anywhere, ever, under any configuration.** It
  exists only transiently in the *product's* request handler, inside
  wisp's `hook`, long enough to compute the day's visitor key, then is
  discarded. wisp's ingest/staging/stats side never receives one.
  wisp's own landing page is a product like any other: its handler
  sees the visitor IP only inside wisp's own hook. This is a hard
  invariant, not a default — treat any code path that logs, stores, or
  forwards a raw IP as a bug, not a missed optimization. The same
  applies to the deployment: no access logs on wisp's nginx or Caddy
  layers (see `deploy/README.md`).
- **The daily salt must never be derivable from what gets persisted.**
  It lives only in the product process's memory (inside its hook) for
  its own UTC day and is discarded at rotation. wisp's ingest side never
  receives, stores, or can request it. wisp's own landing-page hook
  holds product `wisp`'s salt in memory exactly as any product does:
  never persisted, never passed to staging. Visitor keys must be HMACs under that salt, never a
  plain hash: IPv4 × real User-Agents is small enough to brute-force.
  If a day's keys could ever be paired back up with that day's salt,
  the "cannot re-identify a visitor later, even by the operator" claim
  becomes false.
- **Visitor keys never outlive their day at wisp.** Keyed data sits
  only in the day-scoped staging file, which is deleted at day close.
  The product statistics DB never has a key column.
- **Unique visitors are a deliberate lower bound, not an approximation
  of an exact count.** Don't add mechanisms later that quietly chase
  more precision at the cost of the privacy properties above — that's
  a different product with the same name, not an improvement.
- **No cookies, no `localStorage`/`sessionStorage` identifiers, no
  cross-site identifiers.** If a feature seems to need one, the answer
  is "that feature is out of scope," not "find a technically-different
  storage mechanism that has the same effect."

## Relationship to cinder / EphemNet / persona / offgrid

wisp never depends on any of them. The dependency runs one way only:
products import wisp's stdlib-only `hook` package and authenticate
with their own product key and token. wisp never imports or calls a
product. The kinship is otherwise philosophical: the same
identity-free-by-design instinct (see `cinder`'s own T005) applied to
web analytics instead of messaging/hosting/identity.

wisp does share infrastructure with them: the `bh2` server, cinder's
shared nginx skeleton, and EphemNet's `mera.network` DNS. `deploy/`
follows their conventions; see `deploy/README.md`.

## Origin

This repo's idea was previously tracked only in `superplan` (project
`P012`, formed 2026-09-30 from a user conversation about what a web
analytics tool needs). This repo's own `plan/` is now the source of
truth for anything wisp-specific going forward — see `superplan`'s
P012 only for the earlier design-formation history (including the
options considered and rejected before landing on the cookieless-hash
approach).
