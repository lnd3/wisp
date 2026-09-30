# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **P001 Phase 0: deploy `wisp.mera.network`.** The tooling is in
  `deploy/`, adapted from persona/EphemNet/cinder, with ports and
  subnet checked live on `bh2`. It serves a placeholder page until an
  ingest API exists.
- **`hook/` is implemented** (D002 §1b), tested but not yet used by
  any product. There's no ingest API to receive its batches yet.
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

1. Get the `wisp.mera.network` zone entry added (EphemNet side). Then
   run `deploy/configure-nginx.sh bh2 /opt/wisp live` and
   `deploy/deploy.sh bh2 /opt/wisp live`, then the privacy log check.
2. Review D002 (the full data model): settle key fields, salt scope
   and the restart behaviour, and confirm or adjust its proposed
   defaults.
3. Revisit build-vs-adopt. Backend-only aggregate ingest fits
   GoatCounter/Plausible less well than the original snippet design
   did, which likely favours building.
4. Choose storage (SQLite/Postgres).
