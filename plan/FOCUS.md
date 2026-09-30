# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **P001 Phase 0: deploy `wisp.mera.network`.** The tooling is in
  `deploy/`, adapted from persona/EphemNet/cinder, with ports and
  subnet checked live on `bh2`. It serves a placeholder page until an
  ingest API exists.
- **Hook and ingest API are implemented** (D002 §1b–§3): `hook/`,
  `cmd/wisp`, `internal/{ingest,registry,staging}`, and the `wisp`
  service in `deploy/`. Not yet deployed (DNS). No product is wired
  in yet.
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

1. **Day close + product statistics DB (D002 §4–5).** Until it exists,
   the sweep discards each day's staging unaggregated, so no real
   product should be wired in before it lands.
2. Get the `wisp.mera.network` zone entry (EphemNet side). Then:
   configure-nginx, create `products.json`, deploy, and run the
   privacy log check.
3. Wire the hook into a first product (persona is the smallest).
4. Settle D002's open questions: key fields beyond IP+UA, salt scope,
   restart behaviour, and the proposed values.
