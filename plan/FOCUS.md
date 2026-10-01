# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **P001 Phase 0: deploy `wisp.mera.network`.** The tooling is in
  `deploy/`, adapted from persona/EphemNet/cinder, with ports and
  subnet checked live on `bh2`. It serves a placeholder page until an
  ingest API exists.
- **wisp is deployed on bh2** (2026-10-01, `/opt/wisp/live`). Both
  containers run, and ingest, dashboard and logging are verified
  internally. It can't be reached by name yet (no DNS, so no TLS).
- **D001's revision to backend-only ingest.** Products' servers send
  unique events or (preferably) aggregates. No browser ever talks to
  wisp.

---

## Blocked

- **TLS / public reachability.** `wisp.mera.network` needs a zone
  entry in bh2's EphemNet `zones.json` (direct mode, 158.174.211.245).
  That's EphemNet production data and awaits the user's go-ahead.
  Once it exists, restart wisp-caddy so it retries ACME at once.

- **D002 open questions (not blocking deploy).** The wire format is
  implemented. Still open, because they touch CLAUDE.md's invariants:
  key fields beyond IP+UA, salt scope (per-product vs shared), and
  behaviour on a mid-day restart.

---

## Next

1. Add the `wisp.mera.network` zone entry (EphemNet side), restart
   wisp-caddy, then run the remaining checks: real cert, dashboard
   401/200 through Caddy, ingest 401 from outside.
2. Wire the hook into a first product (persona is the smallest): add
   it to `products.json`, then restart wisp.
3. Settle D002's open questions: key fields beyond IP+UA, salt scope,
   restart behaviour, and the proposed values.
