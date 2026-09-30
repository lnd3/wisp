---
id: P001
title: wisp MVP — cookieless pageview/visitor/download analytics, no consent banner
status: PLANNING
priority: MEDIUM
priority_drivers:
  - strategic_edge
created: 2026-09-30
updated: 2026-09-30
depends: []
external_dependencies: []
enables: []
---

## Goal

Build a minimal, self-hosted web analytics tool for the user's own web
products: unique visitors (as a deliberate lower-bound estimate),
pageviews, and file downloads — with the explicit, non-negotiable
constraint that nothing in the design ever requires a cookie-consent
popup. See [[T001]] for the belief this rests on and [[D001]] for the
full technical design.

## Scope

- Included:
  - An authenticated, server-to-server ingest API called by the
    products' own backends — never by end users' browsers — accepting
    both unique events and aggregated event data (aggregates preferred:
    more efficient, and less per-visitor data leaves the product)
  - A product-side contract for what a backend may send (see [[D001]]'s
    2026-09-30 revision — where the visitor hash is computed, event and
    aggregate shapes)
  - Cookieless daily-rotating-salt visitor hashing (no raw IP stored)
  - Deployment at `wisp.mera.network` on the shared `bh2` host
    (`deploy/`, adapted from `persona`/`EphemNet`/`cinder`)
  - Bot/crawler filtering
  - An events store + daily/hourly rollups
  - A dashboard: time series, top pages, top referrers, device
    breakdown, downloads list
- Not included (yet):
  - Any mechanism that would require a consent banner (persistent
    cookies, localStorage IDs, cross-site identifiers) — permanently
    out of scope by design, not just deferred
  - User accounts, multi-tenant auth, or a hosted/SaaS offering — this
    starts as a single-owner, self-hosted tool
  - Real-time dashboards, custom event tracking beyond pageviews/
    downloads, A/B testing — later scope if wanted at all
  - **Portfolio-wide service health monitoring + mobile push alerts** —
    a real, undecided scope-expansion request from `EphemNet` (see
    Log, 2026-09-30); not started, not designed, not committed to.
    Distinct from this project's core web-analytics scope (this would
    be actively polling/receiving health signals from other repos'
    running services — `cinder`, `EphemNet`, etc. — and pushing to a
    lightweight desktop/mobile app, not passive pageview counting) —
    needs its own design decision on whether it belongs in `wisp` at
    all or should be a separate project, before any build starts.

## Linked

- **Theses**: [[T001]]
- **Designs**: [[D001]], [[D002]] (ingest data model + accumulation)
- **Origin**: this repo's idea was previously tracked only in
  `superplan` (project `P012`) — see that repo for the design-formation
  history; this repo's own plan is now the source of truth going
  forward.

## Tasks

### Phase 0 — Deployment
- [x] Deployment tooling adapted from `persona`/`EphemNet`/`cinder`
      (`deploy/`) — ports/subnet checked live on `bh2`
- [ ] `wisp.mera.network` zone entry in `bh2`'s EphemNet `zones.json`
      (EphemNet-side operator step — see `deploy/README.md`)
- [ ] First live deploy (placeholder page) + privacy log check

### Phase 1 — Design decisions
- [ ] Decide build-vs-adopt for real (see D001's Open Questions) —
      confirm a from-scratch build is actually wanted over configuring
      an existing cookieless tool (GoatCounter, Plausible/Umami in
      no-cookie mode)
- [x] Decide where the visitor hash runs: product-side; batches carry
      per-key daily distributions; wisp deletes keys at day close (D001)
- [ ] Pin down exact hash inputs (which browser fields beyond IP+UA,
      if any) and salt scope (per-product vs. shared)
- [ ] Confirm D002's proposed wire format, day close (UTC + 2h grace),
      staging/stats DB layout, and the (proposed) defaults
- [ ] Choose storage (start simple — Postgres/SQLite — defer a
      column-store like ClickHouse until scale actually demands it)

### Phase 2 — Ingest API + product integration
- [ ] Ingest API (unique events + aggregated event data)
- [ ] Product registry: product key + auth token per product (hash-only
      at wisp, server-only file excluded from deploy rsync; D002)
- [ ] `hook` package (D002 §1b): `Start`/`View`/`Download`/`Close`
      and the interval dispatcher. Stdlib-only Go.
- [ ] Wire the hook into a first real product (TBD — persona's landing
      page is the smallest candidate)
- [ ] Bot/crawler filtering (likely product-side now, before aggregation)

### Phase 3 — Storage, rollups, dashboard
- [ ] Events schema + rollup jobs
- [ ] Dashboard UI

## Log

2026-09-30 — Project created at repo bootstrap, seeded from
`superplan`'s `P012`. Status set to `PLANNING` rather than `IDEA` since
the core design constraint (cookieless, no consent banner) is already
decided, not still an open question — what remains is deciding build-
vs-adopt and the concrete implementation, not whether the idea is worth
pursuing at all.

2026-09-30 (later) — Cross-repo request from `EphemNet`, surfaced while
that project discussed its own deferred `deploy/monitor.sh` adaptation
(email-only alerting, parked for lack of a working alert channel): the
user's own framing was "monitor is nice, but it's even better with push
to mobile device, which is always available... we should tell the new
project wisp that will manage web site analytics. It should also
collect service health across our portfolio." Recorded here as a real
ask, not yet designed or scoped — see the new Scope bullet above.
`EphemNet` itself has taken no dependency on this; its own
`monitor.sh` task remains independently blocked on a real alert channel
regardless of whether `wisp` ends up building this.

2026-09-30 (later) — **Direction change from the user: wisp ingests
from product backends, not from end users.** Quote: "wisp api will
accept analytics data from the actual product backends, and not from
users themselves, because of our product-wide privacy protection
guidelines. wisp will accept unique events, as well as aggregated event
data (which is preferable since it's more efficient)." Consequences:
the client JS snippet is out of scope (no browser ever talks to wisp);
the public endpoint becomes an authenticated server-to-server API;
aggregates are the preferred input. Scope and Tasks rewritten
accordingly; [[D001]] carries the architecture revision and the new
open questions (chiefly: where the daily-salted visitor hash is
computed). Same session: deployment tooling added under `deploy/`
(copied from `persona`, itself from `EphemNet`/`cinder`), targeting
`wisp.mera.network` on `bh2` — live ports 9480/9220, subnet
172.27.1.0/24, all checked against `bh2` directly. Not yet deployed:
`wisp.mera.network` has no DNS yet (needs an EphemNet `zones.json`
entry). Residual logging risk outside this repo: the shared nginx
stream skeleton's error log can record `client: <ip>` on backend
connect failures — would need a collaborative edit to cinder's
`sni-shared-passthrough.conf`; much lower stakes now that visitors
never connect to wisp directly.

2026-09-30 (later still) — Ingest batch shape decided (see [[D001]]'s
Log): per-visitor daily distributions keyed by a product-side salted
hash; wisp aggregates at day close and then deletes every key. Two
decisions remain open because they touch this repo's own invariants:
which extra browser fields go into the key, and whether the salt is
per-product or shared.
