# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **P001 Phase 0: deploy `wisp.mera.network`.** The tooling is in
  `deploy/`, adapted from persona/EphemNet/cinder, with ports and
  subnet checked live on `bh2`. It serves a placeholder page until an
  ingest API exists.
- **wisp is live and counting**: cinderapps (cinder's A022, done from
  cinder's side) and wisp's own landing page report into staging. The
  first day closes onto the dashboard at 2026-10-02 02:00 UTC.
  Products register with `deploy/ops.sh … register <key>` (merge-only).
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

1. After 2026-10-02 02:10 UTC: check that the dashboard History shows
   cinderapps, offgridapp and wisp for 2026-10-01, plus the close log
   lines.
2. Uptime monitoring: deployed on `rbserver1:3001`. The user creates
   the admin account, picks a third-party alert channel and adds the
   README's monitors. Then decide on the off-`rbserver1`
   dead-man's-switch instance.
3. EphemNet/persona integrations once they have Go handlers.
4. D002 remaining: confirm the proposed numeric values; country/GeoIP
   undecided.
