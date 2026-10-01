# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **P001 Phase 0: deploy `wisp.mera.network`.** The tooling is in
  `deploy/`, adapted from persona/EphemNet/cinder, with ports and
  subnet checked live on `bh2`. It serves a placeholder page until an
  ingest API exists.
- **wisp is live at https://wisp.mera.network** (bh2,
  `/opt/wisp/live`, build `119ba44`). Public checks have passed. No
  product reports to it yet, so the dashboard shows no closed days.
- **D001's revision to backend-only ingest.** Products' servers send
  unique events or (preferably) aggregates. No browser ever talks to
  wisp.

---

## Blocked

- **D002 open questions (not blocking deploy).** The wire format is
  implemented. Still open, because they touch CLAUDE.md's invariants:
  key fields beyond IP+UA, salt scope (per-product vs shared), and
  behaviour on a mid-day restart.

---

## Next

1. Wire the hook into a first product (persona is the smallest):
   generate its token, add its hash to `/opt/wisp/live/deploy/products.json`,
   restart wisp, set `WISP_TOKEN` in the product's own `.env`, call
   `View`/`Download` at its key handlers, then deploy the product.
2. Watch the first real day close (next day 02:00 UTC + up to 10 min)
   and show on the dashboard.
3. Settle D002's open questions: key fields beyond IP+UA, salt scope,
   restart behaviour, and the proposed values.
