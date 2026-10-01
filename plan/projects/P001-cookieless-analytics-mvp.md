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
    silences its own alert. **Decided 2026-10-01 (user):** wisp owns this. The tool
    was first uptime-kuma, then **reversed the same day: build our own
    lightweight prober** (`uptime-wisp`: Go, stdlib only, on alpine),
    after uptime-kuma's slim image turned out to be 867 MB. Nothing
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

### Phase 4 — External uptime monitoring (own prober, decided 2026-10-01)
- [x] ~~uptime-kuma~~ deployed, then removed (867 MB image; user: "not
      acceptable … build our own lightweight uptime lib")
- [x] `uptime` package + `cmd/uptime-wisp`: HTTP(S) and DNS checks, own
      resolver, cert-expiry warnings, alert on state change (ntfy /
      webhook), optional outgoing heartbeat, status page
- [x] `deploy/uptime/`: 16.8 MB alpine image (vs 867 MB), generic SSH
      deploy (cross-compiles, validates the host config before
      replacing the running prober)
- [x] Deployed on `rbserver1` at `/opt/uptime-wisp` (2026-10-01):
      14 checks, all up; alerts to ntfy topic `lnd_bh2_alerts_84af2f`
      (test alert confirmed delivered); status page `rbserver1.lan:8080`
- [ ] Optional: `heartbeat` to a third-party dead-man service (closes
      offgrid's co-location blind spot on `rbserver1`)

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

2026-10-01 (later) — **uptime-kuma moved to the maintained 2.x line**
(`louislam/uptime-kuma:2`, now 2.5.5), after uptime-kuma's own UI
warned that the `:1`/`latest` tags point at end-of-life v1 with no
security fixes. The v1 database was empty (0 users, monitors and
notifications), backed up anyway to
`/opt/uptime-kuma/backup-v1-20261001T193827Z.tgz`, and migrated
cleanly on first v2 start. The unused v1 image was removed from the
Pi. `deploy.sh` now also prunes dangling images after each update.
The README warns against `1`/`latest`, and to back up before any
major-version switch.

2026-10-01 (later) — Switched to `louislam/uptime-kuma:2-slim` (user):
867 MB on the Pi versus 2.52 GB for `:2`, the same 2.x updates, minus
the embedded MariaDB and Chromium. Neither is needed: the DB is SQLite
and the monitors are HTTP(s)/DNS. Backup before the switch:
`/opt/uptime-kuma/backup-v2-20261001T194627Z.tgz`. The unused full
image was removed. The user questioned the size ("we could build an
uptime image that is 150MB"). Not pursued: uptime-kuma's bulk is
Node and its dependencies, and a self-built image gives up upstream
security updates.

2026-10-01 (later) — **Reversed: our own prober instead of
uptime-kuma.** The user, on learning the slim image is 867 MB (Node,
its dependencies, the Azure/AWS SDKs, cloudflared): "That is not
acceptable … Dump this shit. Let's build our own lightweight uptime
lib that runs on docker alpine." uptime-kuma was removed from
`rbserver1` (container, image, data) and from the repo. Scope is
exactly what this item requires:
- HTTP(S) checks, with the prober's own DNS through a configured
  external resolver.
- DNS checks against a named nameserver.
- TLS verification with expiry warnings.
- Alerts only on state change, via ntfy or a generic webhook (both
  third-party, independent of bh2/rbserver1).
- An optional outgoing heartbeat for a third-party dead-man service.
- A tiny LAN status page.

2026-10-01 (later) — **uptime-wisp built** (image name per the user).
- **Code:** package `uptime` (stdlib only) and `cmd/uptime-wisp`
  (`-once`, `-test-alert`, exit 2 for a bad config). A 7.6 MB static
  binary; the image is **16.8 MB**, running as uid 1000 with a
  read-only root, all capabilities dropped and a 64 MB memory cap.
- **Tests:** state machine (no alert on a single blip, one down and
  one up, certificate warning once per renewal), retries, payloads,
  real HTTP/TLS against httptest, and real DNS against a minimal fake
  UDP server. A mutation check on the repeat-down guard was caught.
- **Live `-once` run:** all product URLs ok. It **found a real EphemNet
  bug**: `ns1`/`ns2.mera.network` return NXDOMAIN publicly, because
  ephemnetd answers its own NS names with a synthesized SOA. The `.network`
  glue keeps delegation working. Flagged to the user, not fixed here.

2026-10-01 (later) — **uptime-wisp live on `rbserver1`**
(`/opt/uptime-wisp`, after the user renamed the old directory).
- **Config:** written on the host only (0600), with the user's ntfy
  topic `lnd_bh2_alerts_84af2f`.
- **State:** healthy, 0 restarts, 25.9 MB image on arm64. All 14
  checks up.
- **Alerts:** a `-test-alert` was confirmed on the topic via ntfy's
  poll API (20:21:17 UTC).
- **ns1 finding:** EphemNet fixed `ns1`/`ns2.mera.network` (EphemNet
  `6b39d4f`) before this went live, so that check is up and stays as
  a regression guard.
- **Memory cap:** docker stats shows 0B memory on the Pi, so the
  kernel's memory cgroup is likely off and `mem_limit` isn't enforced
  there. Harmless at this footprint.
- **Also seen:** EphemNet `009df85` split site serving into
  `ephemnet-site`, which may unblock EphemNet A006 (wisp analytics) if
  it's a Go handler.

2026-10-01 (later) — **uptime-wisp status page gets Basic Auth** (user
request). It's required whenever the page is served, so the service
refuses to start without it.
- **Config:** stores only a SHA-256 of a long random password
  (`-hash-password`; bcrypt isn't in Go's standard library). Both
  user and hash are compared in constant time.
- **`/healthz`** stays open for Docker.
- **New `-check-config`:** validates exactly as the service runs, and
  `deploy.sh`'s pre-flight now uses it. The old `-once` pre-flight
  would have approved an auth-less config and left the container in an
  exit-2 restart loop. Caught before shipping.
- **On rbserver1,** `auth` was merged into the existing config: backup
  taken, every key kept. The password is in
  `/opt/uptime-wisp/.status-password` (0600).
- **Caveat:** Basic Auth over plain HTTP is LAN-grade.

2026-10-01 (later) — **uptime-wisp: "Reload config" button** (user
request), plus SIGHUP.
- **One loader** for startup and every reload: same validation and
  auth rule, so a reload can't accept what a restart would reject. An
  invalid file is rejected with the running config kept, and the error
  is shown on the page.
- **State is kept** for checks whose name, type and target are
  unchanged (no spurious alerts). New, changed and removed checks are
  counted in the result message.
- **Applied between rounds** (a reload waits on the round lock;
  mutation-checked), then a round runs at once.
- **The POST** sits behind the login and is refused cross-site
  (Sec-Fetch-Site/Origin), because browsers attach cached Basic Auth.
  The login is read per request, so a password change applies on
  reload.
- **Config now lives in a mounted directory**
  (`<remote-dir>/config/config.json`): a single-file bind mount would
  miss editors that save by rename. `deploy.sh` migrates the old
  layout once.

