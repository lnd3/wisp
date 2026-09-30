# Plan Changelog

Append-only record of all status and priority changes.

Format: `YYYY-MM-DD | ID | old_status → new_status | note`

---

2026-09-30 | P001 | PLANNING → PLANNING | Scope rewritten: ingest is backend-only (product servers → wisp, unique events + preferred aggregates); client snippet dropped; Phase 0 deployment added (tooling done, not yet live)
2026-09-30 | D001 | PLANNING → PLANNING | Architecture revised for backend-only ingest; new open question on where the visitor hash is computed blocks the ingest contract
2026-09-30 | D001 | PLANNING → PLANNING | Ingest batch shape decided: per-visitor daily distributions keyed by product-side salted hash; wisp deletes keys at day close. Salt scope + extra key fields still open
2026-09-30 | D002 | NEW → PLANNING | Ingest data model: product-side generation, wire format, day-scoped staging, day close, keyless stats DB accumulation (proposed defaults pending user review)
2026-09-30 | D002 | PLANNING → PLANNING | Product integration decided: in-product hook package (github.com/lnd3/wisp/hook) with interval dispatcher that sends only when data is pending
2026-09-30 | D002 | PLANNING → PLANNING | Per-product credentials: product key + auth token; wisp keeps token hashes only in a server-side registry
