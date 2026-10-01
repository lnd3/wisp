---
id: P001
title: wisp MVP — cookieless pageview/visitor/download analytics, no consent banner
status: IN_PROGRESS
priority: MEDIUM
priority_drivers:
- strategic_edge
created: 2026-09-30
updated: '2026-10-01'
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
    Distinct from this project's core web-analytics scope.
    **Shape settled (2026-10-01, user via superplan):** an *external*
    prober that actively checks each product's public endpoint,
    including doing its own DNS resolution. Not products
    self-reporting: a dead machine, lost network or broken DNS can't
    push its own "I'm down". It must run from a vantage point *not*
    co-located with what it checks. **Consequence:** wisp itself runs on
    `bh2` with the products, so the prober can't live in wisp's
    deployment, whichever repo owns the code.
    **Placement candidate (user, 2026-10-01): `rbserver1`**, the
    always-on Pi. It's external for everything on `bh2`, but
    co-located with `offgrid`. Mitigation, as cinder's vigil (T008)
    does: a dead-man's switch, where a receiver *not* on `rbserver1`
    alerts on the prober's silence as well as on reported failures.
    Candidate shape (not decided):
    - The prober on `rbserver1` checks the `bh2` products.
    - A heartbeat receiver on `bh2` (e.g. in wisp) watches
      `rbserver1`.

    Each side covers the other's outage. Probes always target each
    product's real public URL (e.g. `https://offgridapp.mera.network/`),
    never localhost or a LAN address, even for offgrid on the same Pi.
    That keeps the DNS-resolution requirement real (user-confirmed via
    superplan). **Open dependency:** the alert
    delivery path. Push notifications must not route through something
    hosted on `bh2` (or on `rbserver1` alone), or that machine's outage
    silences its own alert. **Decided 2026-10-01 (user):** wisp owns this, and
    the tool is uptime-kuma, reused rather than built custom. Nothing
    else is taken from ailab. Deploy tooling: `deploy/uptime-kuma/`
    (generic, any host over SSH; the user deploys it on `rbserver1`).
    Still open: the alert channel (must be third-party; chosen in
    uptime-kuma's UI) and whether to add the second, off-`rbserver1`
    instance as a dead-man's switch for offgrid's blind spot.

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
- [x] `wisp.mera.network` zone entry in `bh2`'s EphemNet `zones.json`
      (added by the user, 2026-10-01)
- [x] First live deploy (2026-10-01, commit 119ba44): both containers up
      on bh2 at /opt/wisp/live. Verified: ingest 401 and dashboard 200
      on the internal network, http→https 301 via nginx, 0 `remote_ip`
      in Caddy's log, 0 wisp lines in nginx's access.log
- [x] TLS certificate: Let's Encrypt, issued ~15s after a wisp-caddy
      restart once DNS existed; full external checklist passed

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
- [x] Choose storage: SQLite for both staging and stats (D002); defer
      anything heavier until query load demands it

### Phase 2 — Ingest API + product integration
- [x] Ingest API (`internal/ingest`, `cmd/wisp serve`): auth, validation,
      merge into per-product-day staging (`internal/staging`), dedup
- [x] Product registry: product key + auth token per product (hash-only
      at wisp, server-only `deploy/products.json` excluded from deploy
      rsync; `internal/registry`, `wisp hash-token`)
- [x] `hook` package (D002 §1b): `Start`/`View`/`Download`/`Close`
      and the interval dispatcher. Stdlib-only Go (`hook/`).
- [ ] Wire the hook into products, in the user's order (2026-10-01):
      1. `cinder:A022`, cinderapps.org (Go landing server, ready)
         + `offgrid:A012`, offgridapp.mera.network (`cmd/landing`,
         ready; its Caddyfile needs `X-Real-IP` added)
      2. `EphemNet:A006`, eph.network + agent downloads (DEFERRED:
         static pages; unblock options recorded there)
      3. `persona:A010` (DEFERRED: static HTML until persona has a
         web-facing Go service)
- [x] wisp counts its own landing page with its own hook (product
      `wisp`): `internal/site`, served by `wisp serve`
- [ ] Bot/crawler filtering (likely product-side now, before aggregation)

### Phase 4 — External uptime monitoring (P001 scope item, decided 2026-10-01)
- [x] Deploy tooling: `deploy/uptime-kuma/` (compose file, `deploy.sh`,
      README with a first-run monitor checklist). Tested locally
- [x] Deployed on `rbserver1` at `/opt/uptime-kuma` (2026-10-01):
      healthy, 0 restarts, UI on `http://rbserver1.lan:3001`
- [ ] User: create the admin account, pick a third-party alert channel,
      add the README's monitors
- [ ] Decide on the off-`rbserver1` dead-man's-switch instance (offgrid
      blind spot)

### Phase 3 — Storage, rollups, dashboard
- [x] Day close + product statistics DB (D002 §4–5): `internal/stats`,
      `internal/dayclose`, `staging.Summarize`/`Seal`
- [x] ~~Events schema + rollup jobs~~ superseded by D002's staging →
      day close → daily stats tables (no raw event store exists)
- [x] Dashboard UI (`internal/dashboard`, `/dashboard/` behind Caddy
      basic_auth)

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

2026-09-30 (later still) — MVP feature-complete in code: hook →
ingest API → staging → day close → stats DB → dashboard, all tested.
Remaining before it's live: DNS (the EphemNet zone entry), first
deploy, and wiring the hook into a first product. Details in [[D001]]
(dashboard) and [[D002]] (data path).

2026-10-01 — **Deployed to bh2** (`/opt/wisp/live`, commit `119ba44`)
on the user's go-ahead. Server-only config was created then: `.env`
with the domain and the dashboard's bcrypt hash (user `wisp`; the
password is in `/opt/wisp/.dashboard-password`, mode 0600, outside the
synced tree) and an empty `products.json`. nginx fragments are
installed; `nginx -t` passed. Everything on wisp's side verified (see
Phase 0). **Not yet reachable by name:** `wisp.mera.network` still has
no zone entry in EphemNet's live `zones.json`. bh2's ephemnetd answers
it with the same synthesized SOA as an unregistered name, so Let's
Encrypt gets NXDOMAIN. Adding that entry changes EphemNet's production
data, so it was left for the user to authorize.

2026-10-01 (later) — **Publicly live at https://wisp.mera.network.**
The user added the zone entry, and a wisp-caddy restart got a Let's
Encrypt cert in ~15s. External checks against `deploy/README.md`'s
list all passed:
- The landing footer shows build `119ba44`.
- http gives a 301 to https.
- Ingest returns 401 without a token and 405 for GET.
- The dashboard returns 401 without or with wrong credentials, and
  200 with the right ones.
- No `Set-Cookie` anywhere.
- The tester's own IP appears in none of: Caddy's log, wisp's log,
  nginx's access.log.

Phase 0 is done. Next: wire the hook into a first product.

2026-10-01 (later) — **Integration plan, per the user.** Hold off on
persona: its site is static HTML, and the hook needs a Go request
handler. I'd started filing a Go landing server for persona at
`persona.solemn.network`; the user stopped it and asked why persona
would need another web service. Answer: only to feed analytics, which
is the wrong reason to add a service. Target cinder's web first, then
EphemNet's. Integration details went into each product's own plan:
- `cinder:A022` (PLANNING): concrete steps for `cmd/cinderapps` /
  `landingweb.Handler`, with `X-Real-IP` trusted only from the edge
  subnet.
- `EphemNet:A006` (DEFERRED): its pages are static too. It records two
  ways to unblock: a Go handler, or ephemnetd serving the trees.
- `persona:A010` (DEFERRED): pick up when a web-facing Go service
  exists.

No wisp code changes were needed. Registering a product is an
operator step (a token hash in `products.json`, then restart wisp).

2026-10-01 (later) — Also filed `offgrid:A012` (PLANNING) at the
user's request. offgrid is ready too: its deployed container runs
`cmd/landing`, a Go server. Its Caddyfile still says "wisp … runs its
own collector", which predates C001, so the integration adds
`X-Real-IP` and pins `offgridapp-internal`'s subnet for the
trusted-proxy check. persona's A010 already carried the details.

2026-10-01 (later) — **wisp dogfoods its own hook**, per the user
("obviously, we should have the integration in wisp.mera.network
itself"). The landing page moved from Caddy's `file_server` into
`wisp serve` (`internal/site`), so `GET /` is counted with
`View(r, "/")`. Dashboard and ingest traffic are not counted. The hook
reports as product `wisp`, over loopback to the same server (plain
http is allowed only for loopback), and flushes before shutdown.
`X-Real-IP` is trusted only from wisp-caddy, on `wisp-internal`, now
pinned to 172.27.11.0/24 (checked free on bh2). CLAUDE.md's "wisp
itself never receives [a raw IP]" was made precise: the
ingest/staging/stats side still never does, while the landing handler
sees it only inside the hook, like any product's handler. Local
end-to-end: 1 visitor, 2 views, 1 referrer, HEAD not counted, and the
test IP/UA in no stored file and not in the log.

2026-10-01 (later) — **Self-integration deployed (446b473), plus a
registry incident and its fix.**
- **Incident.** While registering product `wisp`, I replaced
  `products.json` with a freshly written file. That wiped the
  `cinderapps` entry a cinder session had added minutes earlier, so
  cinderapps' batches got 401 and were dropped from about 14:35 to
  14:39 UTC. I restored the entry from my own command output and
  checked the hash against the token in cinder's `.env`. `deploy.sh`
  itself was never at fault: rsync excludes the file.
- **Fix:** `deploy/ops.sh … register <product>` (token on stdin, hashed
  locally, only the SHA-256 is sent). It merges and never overwrites,
  is idempotent, allows 2-hash rotation, refuses cross-product hashes,
  backs up to `/opt/wisp/.products-backups/`, validates, swaps
  atomically, and restarts wisp only on change. `ops.sh … registry`
  lists entries. Tested against a scratch registry through a stand-in
  ssh before use.
- **Live state 14:45 UTC:** cinderapps has 4 visitors and 7 views in
  staging; wisp has 1 visitor and 1 view of `/`. The tester's IP is in
  no staging or stats file. Products appear on the dashboard only when
  a day closes, first at 2026-10-02 02:00 UTC. A curl-UA visit isn't
  counted (bot list), which looked like "self-report not working"
  until tested with a browser UA.

2026-10-01 (later) — **Dashboard: "Today so far" and "Issues" tabs**,
per the user.
- **Today so far** summarizes today's open staging per product, with
  the same keyless `staging.Summarize` the day close uses. Visitor
  keys never reach the dashboard; a test renders from a real staging
  store and fails on any key. It shows tiles, bars and histograms,
  where a single day's distinct visitors are honestly "visitors", and
  sums across products are labelled as such. It also has an ingest
  status table: last accepted batch per registered product.
- **Issues:** a new in-memory `internal/events` log (deduplicated,
  bounded, no request data), fed by ingest rejections and failures,
  the day close (retrying / UNAGGREGATED / undeletable), registry
  reload failures and wisp's self-report hook. Errors in the last 24h
  show as a banner on every tab.
- Rendered and inspected in headless Chromium. The mobile Issues table
  was reworked into stacked rows.
- Also updated all four product integration notes (cinder A022,
  offgrid A012, EphemNet A006, persona A010) to register via
  `ops.sh register`.

2026-10-01 (later) — Steering note from superplan
(`superplan/steering/outbox/wisp-todo-20261001T185615Z.md`, M002)
resolves the health-monitoring item's "polling vs receiving"
ambiguity, per the user: an external node actively probes product
uptime, including DNS resolution, rather than products self-reporting.
Added the consequence for this repo: wisp is co-located on bh2, so it
can't host the prober. Ownership and placement remain open. Not a
build request.

2026-10-01 (later) — Second superplan steering note
(`wisp-todo-20261001T190053Z`): the user names `rbserver1`, the Pi
offgrid also runs on, as the prober's placement candidate. The
offgrid blind spot gets a dead-man's-switch mitigation (alert on
silence, receiver not on `rbserver1`). Added here: the symmetric
`rbserver1`-probes / `bh2`-receives candidate, and the alert-delivery
path as the remaining shared dependency. Still not a build request.

2026-10-01 (later) — **Health monitoring decided and tooled.** The user
chose wisp as owner and uptime-kuma as the tool, via superplan's
decision note `wisp-decision-20261001T191904Z`, confirmed directly:
"I want a deploy script that uses uptime-kuma … I can deploy it
wherever I like. But I'll be putting it on rbserver1." Also: "We're
not gonna be using ailab anything, except uptime-kuma."
- **Built** `deploy/uptime-kuma/`: a generic SSH deploy with its own
  Compose project, `--port`/`--bind` remembered on the host, refusal
  of a taken port, pull-and-recreate as the update path, and `data/`
  never touched.
- **Adapted from** superplan's seed (`wisp-seed-20261001T192013Z`):
  dropped its fixed container name, added validation and the port
  guard.
- **Correction:** I first assumed `rbserver1` already ran ailab's
  uptime-kuma, because the SSH user names match. The user is only now
  installing Docker there, so ailab's Pi is a different machine.
- **README:** a first-run checklist covering a third-party alert
  channel and public-URL monitors for every product (all 11 URLs
  verified live), plus a DNS monitor for EphemNet's nameserver,
  certificate-expiry alerts, and the offgrid co-location blind spot.

2026-10-01 (later) — **uptime-kuma deployed on `rbserver1`**
(`/opt/uptime-kuma`, Compose project `wisp-uptime-kuma`, port 3001).
First attempts were blocked until the user installed Docker (29.8.2,
Compose v5.5.1), added `linus4637` to the `docker` group and created
`/opt/uptime-kuma`. Reached from the dev machine as `rbserver1.lan`,
because the bare `rbserver1` doesn't resolve under WSL. Host-key
checking was kept via `HostKeyAlias=rbserver1`. Now waiting on the
user's first-run setup in the web UI.

