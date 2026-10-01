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

1. cinder's A022 (then offgrid's A012, also ready): wire the hook into cinderapps (in the cinder repo),
   register `cinderapps` in `/opt/wisp/live/deploy/products.json`,
   deploy, and watch the first day close onto the dashboard.
2. EphemNet's A006 once its public pages have a Go handler (see that
   action's unblock options); persona's A010 later still.
3. Settle D002's open questions: key fields beyond IP+UA, salt scope,
   restart behaviour, and the proposed values.
