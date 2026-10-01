# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **P001 Phase 0: deploy `wisp.mera.network`.** The tooling is in
  `deploy/`, adapted from persona/EphemNet/cinder, with ports and
  subnet checked live on `bh2`. It serves a placeholder page until an
  ingest API exists.
- **The MVP is implemented in code**: the hook, ingest API, staging,
  day close, stats DB and dashboard, plus the `wisp` service and
  dashboard basic auth in `deploy/`. Not deployed yet (DNS). No
  product is wired in yet.
- **D001's revision to backend-only ingest.** Products' servers send
  unique events or (preferably) aggregates. No browser ever talks to
  wisp.

---

## Blocked

- **First live deploy.** `wisp.mera.network` has no DNS yet. It needs
  a zone entry in `bh2`'s EphemNet `zones.json` (direct mode,
  158.174.211.245), done through EphemNet's operator process.
- **Ingest wire format.** The batch shape is decided (per-key daily
  distributions; keys deleted at day close). Still waiting on two
  decisions that touch CLAUDE.md's invariants: which browser fields
  beyond IP+UA go into the key, and whether the salt is per-product or
  shared.

---

## Next

1. Get the `wisp.mera.network` zone entry (EphemNet side). Then:
   configure-nginx, create `.env` (including the dashboard credentials)
   and `products.json`, deploy, and run the README's verification list
   (privacy log check, ingest 401, dashboard 401/200).
2. Wire the hook into a first product (persona is the smallest).
   Confirm a real day closes and shows on the dashboard.
3. Settle D002's open questions: key fields beyond IP+UA, salt scope,
   restart behaviour, and the proposed values.
