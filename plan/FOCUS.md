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
  first day (2026-10-01) closes on the 16-min-grace deploy, 2026-10-02.
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
2. Uptime monitoring: uptime-wisp is live on `rbserver1:8080`, alerting
   to ntfy. Optional next: a third-party heartbeat (dead-man) for
   offgrid's co-location blind spot. EphemNet's `ephemnet-site` split
   (`009df85`) may unblock its A006 wisp integration.
3. EphemNet/persona integrations once they have Go handlers.
4. D002 remaining: confirm the proposed numeric values; country/GeoIP
   undecided.
