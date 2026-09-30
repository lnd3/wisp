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
  - A collector endpoint receiving pageview and download events
  - A client-side JS snippet: fires pageviews (incl. SPA route-change
    support), listens for download-link clicks
  - Cookieless daily-rotating-salt visitor hashing (no raw IP stored)
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
- **Designs**: [[D001]]
- **Origin**: this repo's idea was previously tracked only in
  `superplan` (project `P012`) — see that repo for the design-formation
  history; this repo's own plan is now the source of truth going
  forward.

## Tasks

### Phase 1 — Design decisions
- [ ] Decide build-vs-adopt for real (see D001's Open Questions) —
      confirm a from-scratch build is actually wanted over configuring
      an existing cookieless tool (GoatCounter, Plausible/Umami in
      no-cookie mode)
- [ ] Pin down exact hash inputs and salt-rotation mechanics
- [ ] Choose storage (start simple — Postgres/SQLite — defer a
      column-store like ClickHouse until scale actually demands it)

### Phase 2 — Core collector + client
- [ ] Collector endpoint (pageview + download events)
- [ ] Client snippet (pageview firing, SPA route-change hook,
      download-link click listener)
- [ ] Bot/crawler filtering

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
