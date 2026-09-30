---
id: D002
title: Ingest data model — generation, day-scoped staging, and accumulation into the product statistics DB
status: PLANNING
project: P001
created: 2026-09-30
updated: 2026-09-30
doc_link: ""
---

## What

The concrete data model behind [[D001]]'s backend-only ingest:
1. What a product backend observes, and how it turns that into
   per-visitor daily records.
2. What goes over the wire to wisp.
3. What wisp holds during a day (keyed, temporary).
4. What it keeps forever (keyless, in the product statistics DB).
5. How that long-lived data accumulates over time and how it can be
   queried.

The shape itself was set by the user on 2026-09-30:
- Batches carry per-visitor daily distributions keyed by a
  product-side hash: pages visited, total visits, page-visit keys.
- wisp aggregates at day close, then removes every hash key.
- The keyless result accumulates into a product statistics DB.

Everything else here marked **(proposed)** is a default filling in
that shape, open to change. The Open Questions list which choices
touch CLAUDE.md's invariants and so need an explicit decision.

Out of scope:
- Anything that links a visitor across days or across products (the
  day-scoped key makes this impossible by construction).
- Cross-tabulations such as page × referrer or page × browser (see
  Key Decisions).
- Real-time counts.

## Why

**Why per-visitor daily distributions rather than fully
keyless aggregates from the product:**
- Unique-visitor counts per page, and "pages per visitor"-style
  distributions, both need to know which hits belong to the same
  visitor.
- A product that pre-aggregates fully would have to run the whole
  day-close computation itself: every product re-implementing
  bucketing, uniques-per-page and so on.

Sending day-scoped keys to wisp centralises that once. The privacy
cost is bounded three ways:
- Keys live at most one day plus grace.
- Keys are HMACs under a salt wisp never has.
- Keys are deleted at close.

Alternative kept available: a product that wants to send nothing
keyed can still send keyless totals (see "Keyless batches"). It loses
the per-visitor distributions.

**Why deltas every few minutes rather than one batch at day end:**
- A single end-of-day batch loses the whole day on a product crash or
  deploy.
- It makes the dashboard a day stale.

Deltas merged by key in wisp cost little, since the merge is just
summing.

**Why the product, not wisp, detects visits (sessions):** only the
product sees the actual request timeline. wisp sees batched deltas,
with no ordering inside them.

## How

### 1. Generation (product side)

For each HTTP request the product serves, it decides whether it's a
**countable hit**:
- **View:** a page response (HTML document or SPA shell) with status
  2xx/304.
- **Download:** a served file from a configured set.

These are not hits:
- Assets, API calls, health checks, redirects, errors.
- Requests from a user agent on the bot list. Bot filtering is
  product-side, before keying.
- The product's own monitoring, such as cinder's `monitor.sh` curls.

For each countable hit:

```
page_key = route template, never the raw path     e.g. "/notes/:id", not "/notes/7Hq…"
                                                  no query string, no fragment
key      = base64url( HMAC-SHA256(salt_d, ipnorm ‖ 0x1F ‖ ua [‖ 0x1F ‖ extra fields…])[:16] )
ipnorm   = IPv4 as-is; IPv6 truncated to its /64                    (proposed)
salt_d   = 32 random bytes, generated in memory at 00:00 UTC of day d,
           never written anywhere, discarded at the next rotation
```

- **Page keys are route templates.** Raw paths can carry identifiers:
  cinder's note IDs *are* capability secrets, and a query string can
  carry an email or a token. The product maps a request to its route
  pattern. wisp rejects page keys containing `?` or `#`, or longer
  than 256 characters.
- **IPv6 /64 (proposed):** privacy extensions rotate the low 64 bits,
  so a full-address key over-counts a single device. /64 roughly
  means "one household or LAN", which fits the deliberate lower
  bound.
- The truncated 128-bit output is plenty to avoid collisions within
  one product-day.

**In-memory accumulator, per product process, for day d:**

```
visitors[key] = {
  last_seen   time        -- for visit detection only; never sent
  visits      int         -- new visit when gap since last_seen > 30 min (proposed)
  views       {page_key: count}
  downloads   {page_key: count}
  referrers   {host: count}   -- external referrer host only, counted on a visit's first hit
  agent       {browser, os, device}   -- coarse families parsed from UA, first seen wins
}
pending = the same shape, holding only increments since the last flush
```

Flushing and rollover:
- **Every 5 minutes (proposed)**, the product flushes `pending` as a
  batch and clears it.
- **At 00:00 UTC** the product flushes day d's final `pending`, then
  discards `visitors`, `pending` and `salt_d`, and starts day d+1
  with a fresh salt.
- A visit that spans midnight counts once in each day; the key is new
  anyway.

### 1b. The hook: what a product actually integrates (decided 2026-09-30)

Per the user: wisp provides a **simple hook** that products call at
key locations. It **runs inside the product**, and a background
dispatcher sends ingestion calls **on a time interval, only when
there's data to send**. Everything in §1 (keying, accumulator, visit
detection, rollover) lives inside the hook. The product itself only
decides *where* a hit happens and *what route* it was.

Package `github.com/lnd3/wisp/hook`: Go (cinder, EphemNet, persona
and offgrid are all Go 1.24+), **standard library only**, so adopting
it adds no transitive dependencies to a product.

```go
// once, at startup
w, err := hook.Start(hook.Config{
    Endpoint: "https://wisp.mera.network/v1/ingest", // empty → hook is a no-op (dev/local)
    ProductKey: "cindernote",               // the product's identity at wisp
    Token:      os.Getenv("WISP_TOKEN"),    // its secret; never hardcoded or committed
    Interval: 5 * time.Minute,                         // default
    ClientIP: api.ClientIP,  // optional: the product's own trusted-proxy logic; default r.RemoteAddr
})
defer w.Close(ctx) // final flush of pending data, then stop

// at key locations — e.g. the handler that serves a page, or a file
w.View(r, "/notes/:id")
w.Download(r, "/files/:name")
```

**Hot path (`View`/`Download`):**
- It returns immediately, with no network I/O, no error return and no
  logging. Under one mutex it:
  1. Drops bots.
  2. Reads the client IP and UA from `r`.
  3. Computes the HMAC key.
  4. Updates the accumulator and `pending`.
- The raw IP never leaves this call. The hook never logs, and never
  puts an IP or a key into an error it reports.
- Hits for a new UTC day switch to the new day's salt and accumulator
  right inside the call, so the rollover doesn't depend on the
  dispatcher's timing.

**Dispatcher (one goroutine, started by `Start`):**
- **Every `Interval`,** if `pending` is empty it does nothing: no
  request at all, so an idle product produces no traffic. Otherwise it
  swaps `pending` out and sends it as one batch with a fresh
  `batch_id`.
- **Just after 00:00 UTC** it sends the finished day's final `pending`
  at once. It doesn't wait for the next tick, which gives the most
  margin before wisp's close.
- **On failure** (network error, `5xx`) it keeps the batch, with the
  *same* `batch_id` so wisp's dedup makes the retry safe, and retries
  on later ticks with backoff.
- **It drops a batch** on `409`, meaning the day is already closed
  (lower bound preserved), or once the batch's day is past its close
  deadline.
- **Memory is bounded:** unsent batches are capped, and the oldest are
  dropped first. A long wisp outage costs data, never product memory
  or latency.
- **Errors** go only to an optional `OnError func(error)` and to
  counters (`w.Stats()`: sent, dropped, retrying). There's never a log
  line with request data.

**Close(ctx)** flushes `pending` once, on graceful shutdown, and then
stops. Any hits after that are ignored. A crash or kill loses
whatever was unsent, bounded by `Interval`.

What a product still owns:
- Choosing the call sites.
- The route template for each call site (§1's page-key rule: never
  the raw path).
- Supplying `ClientIP` if it sits behind a proxy. Every product on
  `bh2` sits behind Caddy, so the default `r.RemoteAddr` would be
  Caddy's address. That's not a privacy failure, but uniques collapse
  to about 1 per day. The hook counts hits from private or loopback
  client addresses (`Stats().PrivateClientIPs`). Once they're the
  majority of the day's hits, it reports `ErrLikelyProxyAddress` once
  to `OnError`. It does this without keeping any address.

### 2. Wire format

`POST https://wisp.mera.network/v1/ingest`, with
`Authorization: Bearer <token>`. The body's `product` is the product
key.

**Product credentials (decided 2026-09-30, per the user):** every
product has a **product key** (a public identifier, e.g.
`cindernote`) and an **authentication token** (a secret). wisp accepts
a batch only if the token matches the one registered for that product
key.
- **At wisp:** a product registry, `product_key → token hashes`. wisp
  stores only a SHA-256 of each token and compares in constant time.
  Unknown key, missing token or wrong token all get the same `401`, so
  a caller can't probe which product keys exist.
- **Rotation (proposed):** a product may have two valid tokens at
  once, so it can switch to the new one before the old one is revoked.
- **Scoping:** a token only ever writes its own product's data. The
  staging file and stats rows are chosen by the authenticated product
  key, never taken on trust from the body.
- **Generating a token:** `openssl rand -hex 32`, the same convention
  as EphemNet's `EPHEMNETD_PAID_REGISTRATION_TOKEN`. The product keeps
  it in its own `deploy/.env` as `WISP_TOKEN`. The wisp side keeps
  only the hash, in a server-only registry file excluded from
  `deploy.sh`'s rsync, the way EphemNet treats `zones.json`.

```json
{
  "product": "cindernote",
  "day": "2026-10-01",
  "batch_id": "0192f1c4-…",          // unique per batch; wisp dedups on (product, day, batch_id)
  "visitors": [
    {
      "k": "Q3xV9…",                 // 22 chars, base64url of 16 bytes
      "visits": 1,
      "views": { "/": 1, "/notes/:id": 1 },
      "downloads": {},
      "referrers": { "news.ycombinator.com": 1 },
      "agent": { "browser": "firefox", "os": "linux", "device": "desktop" }
    }
  ]
}
```

- **A "unique event"** is just a batch with one visitor and one view.
  The format is the same; the product simply flushes per hit.
- **Keyless batches** (for a product that opts out of per-visitor
  data): `"totals": {"views": {...}, "downloads": {...},
  "visits": n}` with no `visitors` array. These feed only the
  pageview and download counts, not uniques or distributions.
- **Limits (proposed):**
  - At most 10,000 visitors per batch.
  - At most 500 distinct page keys per visitor.
  - `day` must be an open day (see Day close); anything else gets
    `409`.

### 3. Staging (wisp, day-scoped, keyed)

- **One SQLite file per `(product, day)`:**
  `staging/<product>/<day>.sqlite`. It is created on the first batch
  and **deleted as a whole file** at close, not row-by-row, so no
  page or free-list remnant survives inside a long-lived database.
  `PRAGMA secure_delete=ON` is set anyway.
- **Merging** is additive: a batch's `visits`, `views`, `downloads`
  and `referrers` are summed into the key's existing row, and `agent`
  keeps whatever was seen first.
- **Dedup:** `applied_batches(batch_id)` sits in the same file and is
  checked in the same transaction as the merge. A retried batch is a
  no-op.

```
staging_visitor(key PK, visits, browser, os, device)
staging_hit(key, kind['view'|'download'], page_key, count,  PK(key, kind, page_key))
staging_ref(key, host, count,  PK(key, host))
applied_batches(batch_id PK)
```

### 4. Day close (wisp)

Day d for a product closes at **d + 24h + grace, with a grace of 2h
(proposed)**. That leaves margin for the midnight final flush and
retries. After close, batches for d get `409` and the product drops
them; a lower bound stays a lower bound.

In one transaction against the stats DB, computed from that day's
staging file:

| Stat | Computation |
|---|---|
| `uniques` | `count(*) from staging_visitor` |
| `visits`, `views`, `downloads` | sums |
| `bounces` | visitors with exactly one view in total |
| per page: `views`, `uniques` | `sum(count)`, `count(distinct key)` per `(kind, page_key)` |
| per referrer host: `visits`, `uniques` | `sum(count)`, `count(distinct key)` |
| per agent dimension/value: `uniques` | e.g. browser=firefox → visitors |
| histograms | visitors per bucket of distinct pages, of views, of visits |

Histogram buckets (proposed): `1, 2, 3, 4–5, 6–10, 11–20, 21+`, and
`1, 2, 3, 4+` for visits.

Then commit, then **unlink the staging file**. Close is idempotent:
- Stats rows are upserted by their natural key.
- A crash between commit and unlink just re-runs the close with the
  same result.
- A startup sweep deletes any staging file whose day is past close.

### 5. Product statistics DB (wisp, durable, keyless)

No column anywhere holds a visitor key. Each row is one product-day,
written once at close and never updated afterwards:

```
daily_totals  (product, day, uniques, visits, views, downloads, bounces)
daily_page    (product, day, kind, page_key, views, uniques)
daily_ref     (product, day, host, visits, uniques)
daily_agent   (product, day, dim['browser'|'os'|'device'], value, uniques)
daily_hist    (product, day, metric['pages'|'views'|'visits'], bucket, visitors)
```

**How it accumulates, and what can and can't be summed over a
window:**

- **Additive, so sums are valid for any window:** `views`,
  `downloads`, `visits`, `bounces`, and histogram `visitors`. A
  histogram summed over a window is a histogram of *visitor-days*.
- **Not additive:** `uniques`. Summing 30 daily uniques gives
  *visitor-days*, not "unique visitors this month". The same person
  on 5 days is 5 different keys, by design.
  - The dashboard labels multi-day figures "visitor-days" or "avg
    daily visitors", never "unique visitors".
  - A true multi-day unique count is impossible here, and that's the
    point.
- **Per-page uniques** follow the same rule: a sum over days is
  visitor-days for that page.
- **Across products:** with per-product salts, the same visitor on
  two products is two keys. A portfolio view is the sum of product
  rows, again visitor-days. A shared salt would change this (see Open
  Questions).
- **Retention:** indefinite. The rows are counts with no personal
  data.
- A monthly rollup table (sums of the additive columns, plus avg
  daily uniques) is added only when query load needs it.

### Worked example

A visitor on cindernote, 2026-10-01, arriving from Hacker News:

```
09:00  GET /            → view "/",           new visit (visits=1), referrer news.ycombinator.com
09:02  GET /notes/7Hq…  → view "/notes/:id"   (same visit)
14:30  GET /            → view "/",           gap 5.5h > 30 min → visits=2
```

- **09:05 flush:** `{k, visits:1, views:{"/":1,"/notes/:id":1},
  referrers:{"news.ycombinator.com":1}, agent:{firefox,linux,desktop}}`
- **14:35 flush:** `{k, visits:1, views:{"/":1}}`, merged in staging
  to `visits:2, views:{"/":2,"/notes/:id":1}`.
- **2026-10-02 00:00:** the product discards the salt. Key `k` can no
  longer be regenerated by anyone, the product included.
- **2026-10-02 02:00, close:** this visitor adds:
  - `uniques += 1`, `views += 3`, `visits += 2`, `bounces += 0`
  - page `/` gets views +2 and uniques +1; `/notes/:id` gets +1 and +1
  - referrer HN gets visits +1 and uniques +1
  - firefox, linux and desktop each get uniques +1
  - histograms: pages bucket `2`, views bucket `3`, visits bucket `2`

  Then the staging file is unlinked, and `k` exists nowhere.

## Where

**Architectural placement:**
- **Product side:** the `hook` package (§1b), linked into each
  product. The product calls `hook.Start` once and
  `View`/`Download` at key locations. The hook's own goroutine sends
  to wisp. The dependency points one way only (product → wisp's
  `hook` package). wisp itself never imports a product, per
  CLAUDE.md.
- **wisp side:** the ingest handler (auth → validate → merge into
  staging), the close scheduler, and the dashboard API reading the
  stats DB.

**Data ownership:**

| Data | Where | Lifetime | Category |
|---|---|---|---|
| Raw IP, UA | product request handler | one request | transient input |
| `salt_d` | product process memory | day d only | secret; never at wisp |
| accumulator | product process memory | day d only | runtime state |
| product key | product config + wisp registry | product lifetime | configuration (not secret) |
| auth token | product's `deploy/.env` (plaintext) | until rotated | credential |
| token hash | wisp registry file (server-only) | until revoked | credential verifier |
| staging file | wisp volume | ≤ d + 26h | keyed runtime state |
| stats DB | wisp volume | indefinite | durable product data, keyless |

**Lifecycle:**
- A product process that restarts mid-day gets a new salt, so its
  returning visitors get new keys and count twice that day: an
  over-count, against the lower-bound intent. See Open Questions.
- The wisp close scheduler must run a startup sweep before accepting
  ingest, so no expired staging file ever lingers.

## Constraints

- No visitor key in any stats DB table. A test asserts the schema has
  no key column, and that after a close no staging file exists for
  that day.
- Every ingest request is authenticated as exactly one product key.
  Data is filed under the authenticated key, not the body's claim.
  wisp stores token hashes, never tokens.
- wisp never receives, stores or can request a salt. There's no
  endpoint for it.
- Page keys are route templates. wisp rejects `?`/`#` and over-long
  keys, but correctness depends on the product's route mapping.
- No cross-tabulated breakdowns in the stats DB, only
  one-dimensional ones (page, referrer, agent dimension). Small cells
  in cross-tabs (a rare referrer × rare page × rare browser) can
  single a person out even without keys.
- Uniques are never summed across days and presented as uniques.

## Migration

N/A. Greenfield.

## Testability

- **Hook:**
  - The hot path does no I/O (a fake transport records calls).
  - An empty interval produces no request.
  - A failed send is retried with the same `batch_id`.
  - A `409` drops the batch.
  - The unsent-batch cap is enforced.
  - The midnight rollover sends the finished day at once.
  - `Close` flushes.
  - A no-op when `Endpoint` is empty.
  - A test transport asserts no request body ever contains the test
    client's raw IP.
- **Product library:**
  - Keying is deterministic given a salt: the same inputs give the
    same key, and a different salt gives a different key.
  - Visit detection runs off a fake clock.
  - Rollover discards state: after midnight, the old key can't be
    produced.
- **wisp:**
  - Merge and dedup: the same batch twice changes nothing.
  - Close against fixture staging files gives exact expected stats
    rows, and the staging file is gone.
  - Idempotent re-close.
  - `409` for a closed day.
  - Schema test: no key columns.
- **End to end:** the worked example above as a literal test case.

## Key Decisions

- **Explicit hook calls at chosen locations over automatic HTTP
  middleware (user's direction):** only the product knows which
  responses are real page views or downloads, and what the route
  template is. Middleware would guess at both, and a wrong guess about
  the page key is a privacy bug (raw paths). A thin optional
  middleware helper can come later if products want it.
- **Interval-based sending, skipped when empty (user's direction):**
  there's no per-hit network call, so the product's request latency
  never depends on wisp, and an idle product costs nothing.
- **Route-template page keys over raw paths:** raw paths leak
  capability IDs and query-string PII. Tradeoff: per-note or per-file
  granularity is lost unless the product deliberately exposes a
  non-secret name.
- **Deltas every ~5 min over one end-of-day batch:** crash and deploy
  tolerance, plus a fresh dashboard. Tradeoff: the key is held at
  wisp during the day rather than only at close.
- **Whole-file staging per product-day over rows in a shared DB:**
  deleting keys means unlinking a file, not trusting row deletion to
  scrub pages.
- **One-dimensional breakdowns only:** limits re-identification from
  small cells. Tradeoff: no "which referrers led to which pages".
- **Visit = 30-min inactivity gap, detected product-side (proposed):**
  a common convention, and only the product sees request order.

## Open Questions

- **Key fields beyond IP+UA:** each added field makes keys more
  distinct, trending from a lower bound toward fingerprinting (see
  D001). The default here is IP+UA only until decided.
- **Salt scope:** per-product (the default here) vs. shared across
  products. Shared allows portfolio-wide uniques, but the products
  must exchange a salt wisp never sees. Also: a product running
  several replicas needs them to share one salt.
- **Restart mid-day:**
  - Accept the over-count, which is the default.
  - Or keep the salt in RAM outside the process (e.g. tmpfs),
    surviving restarts but not reboots. This relaxes "salt never
    leaves the process" slightly.
- **Grace length (2h?), flush interval (5 min?), histogram buckets,
  batch limits:** all proposed values.
- **Country/GeoIP:** still open from D001. If added, it's
  product-side, as one more coarse `agent`-style dimension.
- **Stats DB engine:** SQLite (single file, matches staging) vs.
  Postgres. Nothing above depends on it.

## Related

- Project: [[P001]]
- Architecture and invariants: [[D001]]
- Thesis: [[T001]]

## Log

2026-09-30 (later) — **§2–§3 implemented**: the ingest API
(`internal/ingest`), the registry (`internal/registry`), staging
(`internal/staging`), and the `cmd/wisp` binary (`serve`,
`hash-token`), wired into `deploy/`. Tests cover the handler, registry
and staging, plus the real hook → handler end to end. Also verified:
the binary and the Docker image, with ingest, dedup, 401 and graceful
stop. Decisions beyond the text above:
- **Auth before body:** the token is looked up by its SHA-256 (tokens
  are 256-bit random, so the map-lookup timing reveals nothing).
  Unauthenticated requests are refused without reading the body. A
  body whose `product` differs from the token's product gets the same
  `401`.
- **Validation:**
  - Unknown JSON fields are rejected, and bodies are capped at 16 MiB.
  - Visitor keys must be exactly 22 base64url characters.
  - A visitor must carry at least one view or download; otherwise it
    would inflate uniques.
  - Page keys follow `hook.ValidPageKey`.
  - Referrers must be bare hosts, at most 100 per visitor.
  - Agent fields must match `[a-z0-9]{1,32}`, so a full User-Agent
    can't be smuggled in.
  - The day must be open, with up to 10 min of future clock skew.
- **Staging files** are 0600 in 0700 directories. They use
  `secure_delete=ON` and the rollback journal (not WAL), so all keyed
  data sits in one file plus a transient `-journal`. `Delete` removes
  every side file. Product key and date are re-validated before
  becoming path segments.
- **Sweep, the interim behaviour:** it runs at startup and every 10
  min, deleting any staging file past day start + 26h. The day close
  (§4) doesn't exist yet, so **those days are discarded unaggregated**.
  That's deliberate: keys must not outlive their day. It's why §4–5
  must land before a real product is wired in.
- **Deploy:** `wisp` runs on an `internal: true` network (no egress).
  `wisp-caddy` routes `/v1/*` to it. The registry is a server-only
  `deploy/products.json`. The SQLite driver is `modernc.org/sqlite`
  v1.34.5 (pure Go, as cinder and offgrid use); newer releases require
  Go ≥ 1.24–1.26.

2026-09-30 (later) — **§1b implemented** in `hook/` (module
`github.com/lnd3/wisp`, `go 1.23`, so it builds locally and is
importable by the products' 1.24+). 95% statement coverage, race
detector clean, and two deliberate mutations caught (a skipped salt
wipe, and a referrer counted mid-visit). What's in the code beyond
the text above:
- `Start` rejects plain-http endpoints except loopback, so a token is
  never sent in clear.
- Misuse warnings (`ErrInvalidPageKey`, `ErrLikelyProxyAddress`) are
  reported once per UTC day, not per hit, and never include the
  offending page key, which might be a raw path.
- An idle midnight retires the day without creating a new salt; the
  next hit creates one lazily.
- 408/429/5xx and network errors retry. 409 gives `ErrDayClosed`, and
  any other 4xx gives `ErrRejected`; both drop the batch.
- `Close` makes one forced send attempt and returns an error counting
  the batches it dropped.
- The `MaxVisitorsPerDay` cap (default 200,000) bounds accumulator
  memory, alongside the 500-pages-per-visitor and 288-queued-batch
  caps.
- `wisp-hook` is on the bot list, so one wisp hook's traffic is never
  counted by another.

2026-09-30 (later) — Product credentials, per the user: each product
has a product key and an authentication token that wisp accepts.
Specified: a registry holding only token hashes, a uniform `401`, two
live tokens per product for rotation (proposed), and data scoped by
the authenticated key. `hook.Config` now takes `ProductKey` + `Token`.

2026-09-30 (later) — Added §1b, the hook, per the user: "a simple hook
for products to use at key locations… run within the product
regularly and dispatch ingestion calls at time intervals, if there's
data to ingest." Defined as the stdlib-only Go package
`github.com/lnd3/wisp/hook`: `Start`/`View`/`Download`/`Close`, a
non-blocking hot path, and a dispatcher that ticks every interval,
sends only when something is pending, and retries with a stable
`batch_id`.

2026-09-30 — Design created on the user's request to spell out what
the data is, how it's generated and how it's accumulated. It builds on
their stated shape (per-visitor daily distributions keyed product-side
→ wisp aggregates and drops keys at day close → keyless aggregates
accumulate in the product statistics DB). Values marked (proposed) are
defaults for the user to confirm.
