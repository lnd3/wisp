---
id: D001
title: Cookieless, ephemeral analytics — collection, hashing, storage, and rollup architecture
status: PLANNING
project: P001
created: 2026-09-30
updated: 2026-09-30
doc_link: ""
---

## What

How to count unique visitors, pageviews, and downloads on a website
without any mechanism that legally requires a cookie-consent popup —
no persistent cookie, no localStorage identifier, no cross-site
tracking, no raw IP retention. Explicitly out of scope: exact visitor
counts, cross-session user journeys, cohort/retention analysis, or
anything requiring a durable per-person identity. This design produces
a **lower-bound estimate**, by construction, not an approximation of an
exact count.

## Why

Cookie-based analytics is the default because it's accurate and easy,
not because it's necessary for what most site owners actually use
analytics for (see [[T001]]). The trade-off this design makes on
purpose: give up exact, durable identity in exchange for never needing
a consent banner and never storing anything that could re-identify a
specific visitor later, even to the operator.

**Alternatives considered and rejected:**
- **First-party cookie with a random UUID** — accurate, standard, but
  requires consent under GDPR/ePrivacy once used for anything beyond
  strictly-necessary functionality. Rejected outright — this is exactly
  the popup the whole project exists to avoid.
- **localStorage-based ID** — same consent problem as a cookie, just
  moved to a different storage mechanism; doesn't avoid the underlying
  legal trigger.
- **Client-side fingerprinting (canvas/audio/WebGL)** — could produce a
  more stable identifier than IP+UA, but actively fights the browser
  rather than using only what it passively hands over, and is a heavier
  privacy trade-off in the opposite direction from this project's
  stated intent. Rejected; see T001's "What comes after" for when this
  might become the least-bad option anyway.
- **Server-log-only analysis (no client JS at all)** — simplest,
  catches every request including non-JS clients, but can't see SPA
  route changes or distinguish real downloads from other file requests
  without extra correlation. Not rejected — used as the download-
  tracking mechanism specifically (see How, below), just not sufficient
  alone for pageviews on a JS-heavy site.

## How

**Unique visitor estimate**: `dayHash = HMAC(dailySalt, IP + "|" + UserAgent)`.
- `dailySalt` is generated fresh at each rotation boundary (see Where,
  Data ownership) and is never logged, persisted in a queryable form
  alongside events, or written to backups in a way that could later be
  paired back up with that day's raw request data.
- The raw IP is used only transiently inside the request handler to
  compute the hash (and, optionally, a country lookup) and is discarded
  immediately after — never written to any log, event row, or backup.
- A "unique visitor" for a given day/page/site is a distinct `dayHash`
  value seen that day. This is explicitly a lower bound: shared IPs
  (NAT, corporate networks, carrier-grade NAT) under-count distinct
  people as one; the same person across two calendar days over-counts
  as two. Both directions are accepted, not corrected for.

**Pageviews**: client snippet calls the collector on:
- Initial page load (normal navigation).
- SPA route changes — hook `history.pushState`/`popstate` (and
  framework-router events where relevant) to fire a synthetic pageview,
  since there's no full navigation to observe otherwise.
- Each event: `{timestamp, path, referrer, dayHash, ua_family, country?}`.
  No session table, no server-side session state — if a "session" metric
  is ever wanted, derive it after the fact from consecutive same-
  `dayHash` pageviews within a time window, computed at query/rollup
  time, not tracked live.

**Downloads**: two complementary paths, not one:
- Client-side: a click listener on links matching a configurable list of
  file extensions, firing a "download" event (same shape as a pageview,
  `event_type: download`) before/on click.
- Server-side (preferred where applicable): if downloads are served as
  direct file URLs, parse the actual file-serving request — catches
  direct/shared links and non-JS clients that the click listener alone
  would miss. Correlate with the `Referer` header on that request for
  "which page they came from," when present.

**Bot/crawler filtering**: apply a maintained bot-UA list before
counting anything as a visitor/pageview/download. Without this, the
"lower bound" framing breaks down differently — not under-counting real
humans, but over-counting crawler noise as if it were traffic.

**Storage**: an append-only events table —
`(id, timestamp, event_type, path, referrer, day_hash, ua_family, country, site_id)`.
Start on Postgres or SQLite; defer a column-store (ClickHouse, what
Plausible/Umami use at scale) until real query load justifies the
added operational complexity — premature for an MVP.

**Rollups**: raw-event queries get slow at any real volume. Pre-
aggregate into daily (and optionally hourly) rollups: unique
`day_hash` count per site/day, pageviews per path per day, top
referrers per day. This is the same tiered-rollup shape already used
in `offgrid`'s telemetry storage — worth reading that implementation
for the rollup-scheduling pattern specifically, even though the domain
is unrelated.

**Dashboard**: time-series chart (visits/pageviews over time), top
pages, top referrers, device/browser breakdown (from `ua_family`), a
downloads list. No real-time view in the MVP — real-time requires
either polling raw events directly (defeats the point of rollups) or a
separate live-counter mechanism; deferred.

## Where

**Architectural placement**: a small standalone service — a collector
HTTP endpoint, a rollup job (cron or a background goroutine on a
timer), and a dashboard API/UI. No dependency on the sites being
tracked beyond the client snippet they embed.

**Data ownership**:
- Raw IP: transient, request-scoped only. Never persisted.
- Daily salt: generated and held only for its own 24h rotation window;
  discarded after rotation completes (or after some short grace period
  to handle events straddling the boundary — see Open Questions).
  Configuration-adjacent, not user data.
- Events (`day_hash`, path, referrer, etc.): the actual stored state,
  lifetime governed by a retention policy (raw events could reasonably
  be pruned after rollups are computed, keeping only the aggregates
  long-term — exact retention window not yet decided).
- Rollup tables: durable, long-lived — these are the actual analytics
  product surface.

**Initialization & lifecycle**: the salt-rotation mechanism needs to be
running (and correct) before the collector accepts any events — an
uninitialized or stale salt would either reject events or, worse,
silently hash against a salt from the wrong window. Rollup jobs run
independently on their own schedule, reading only already-committed
events.

## Constraints

- No code path may persist a raw IP address, under any configuration.
  This isn't a default to be overridden — it's a hard invariant worth
  testing for directly (e.g. a test that greps stored data for
  IP-shaped strings and fails if any appear).
- No code path may set a cookie or write to `localStorage`/
  `sessionStorage` from the client snippet.
- The daily salt must not be derivable from anything that gets
  persisted alongside events — otherwise "cannot re-identify a visitor
  later" becomes false in practice even if true in intent.

## Migration

N/A — greenfield project, no existing data or callers to migrate.

## Testability

The hashing/rotation logic is pure enough to unit test directly (given
an IP, UA, and salt, assert the hash; given two different salts,
assert two different hashes for the same IP/UA). The collector and
rollup logic should be testable against a fixture set of raw events
without needing a live browser — the client snippet's SPA-route-change
and click-listener behavior is the one piece that likely needs a real
or headless-browser test, similar to how `offgrid`'s dashboard JS was
verified with jsdom rather than assumed correct from reading the code.

## Key Decisions

- **Lower-bound estimate over exact count**: chosen deliberately (see
  T001) — the accuracy given up is accuracy most site owners don't
  need, in exchange for never needing a consent banner.
- **Server-side download tracking preferred over client-only**: more
  reliable (catches non-JS/direct-link cases) even though it requires
  downloads to be served through a path this system can observe.
- **Rollups over live aggregate queries**: standard analytics-workload
  pattern, and directly reuses a pattern already proven in `offgrid`.

## Open Questions

- **Salt rotation boundary**: strict UTC midnight vs. a rolling 24h
  window from first use — affects how a visitor active right at the
  boundary is counted (splits into two hashes either way). Not decided.
- **Build vs. adopt**: still open at the project level (see [[P001]]'s
  Tasks) — this design assumes a from-scratch build but doesn't yet
  justify that over configuring GoatCounter/Plausible/Umami in their
  existing no-cookie modes.
- **Country/geography**: worth the added GeoIP-database dependency for
  a system whose whole premise is doing as little as possible with the
  request? Not decided.
- **Raw event retention window**: how long to keep raw events once
  rollups are computed from them — balances "ability to recompute
  rollups differently later" against "store as little as possible for
  as short as possible," which is this project's own stated ethos.

## Related

- **Project**: [[P001]]
- **Thesis**: [[T001]]
- **Prior art referenced, not adopted**: GoatCounter's cookieless
  salted-hash implementation — worth reading directly for exact
  window/hash-input details before finalizing this design's own choices.
- **Origin**: `superplan`'s `P012` holds the design-formation history
  from before this repo existed.

## Log

2026-09-30 — Design created at repo bootstrap, expanding `superplan`'s
`P012` into a full design doc now that this repo exists as the source
of truth for it.
