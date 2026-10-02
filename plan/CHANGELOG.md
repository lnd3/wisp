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
2026-09-30 | C001 | NEW → STABLE | No visitor-side third-party requests: portfolio-wide rule behind backend-only ingest (user's stated rationale)
2026-09-30 | D002 | PLANNING → PLANNING | hook package (§1b) implemented in hook/ — tested, not yet adopted by a product; ingest API still to build
2026-09-30 | D002 | PLANNING → PLANNING | Ingest API implemented (cmd/wisp, internal/{ingest,registry,staging}) and wired into deploy/; interim sweep discards unaggregated days until the day close (§4–5) exists
2026-09-30 | D002 | PLANNING → PLANNING | Day close + stats DB implemented (internal/stats, internal/dayclose); stats engine resolved: SQLite. Interim unaggregated sweep removed
2026-09-30 | P001 | PLANNING → PLANNING | Dashboard implemented; MVP complete in code (hook → ingest → staging → day close → stats → dashboard). Remaining: DNS, deploy, first product integration
2026-10-01 | P001 | PLANNING → IN_PROGRESS | Deployed to bh2 /opt/wisp/live (119ba44); verified internally; public TLS blocked on wisp.mera.network DNS entry (EphemNet zones.json)
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | Publicly live at https://wisp.mera.network (DNS added by user, LE cert issued); Phase 0 complete; external checklist passed
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | Integration order set by user: cinder:A022 (cinderapps) → EphemNet:A006 (deferred, static) → persona:A010 (deferred, static); details filed in each repo's plan
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | offgrid:A012 filed (ready: cmd/landing is Go); persona:A010 already had details
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | wisp counts its own landing page with its own hook (product wisp, internal/site); wisp-internal subnet pinned
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | Self-integration live; registry overwrite incident (cinderapps entry wiped ~4 min, restored) → merge-only ops.sh register
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | Dashboard gains Today-so-far (keyless live staging summary + ingest status) and Issues (in-memory event log, 24h error banner); product notes now use ops.sh register
2026-10-01 | D002 | PLANNING → PLANNING | Open question resolved by user: product restart mid-day → accept double count; salt stays process-only (no tmpfs persistence)
2026-10-01 | D002 | PLANNING → PLANNING | User decisions: per-product salts; IP+UA only as key inputs (other headers reviewed and rejected). D001's implemented questions marked resolved
2026-10-01 | D001 | PLANNING → PLANNING | Histogram charts explained in plain language (question titles, what-a-bar-counts, axis caption, takeaway) after user feedback
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | Health-monitoring scope item: shape set to external active prober incl. DNS (user, via superplan steering note); can't be hosted on bh2/wisp; ownership + placement still open
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | Health monitoring: placement candidate rbserver1 (user); dead-man's-switch for offgrid co-location; open: alert delivery path must not depend on bh2/rbserver1 alone
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | Health monitoring decided (user): wisp owns it, uptime-kuma; deploy/uptime-kuma/ tooling added (generic SSH deploy, tested locally); user deploys on rbserver1
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-kuma deployed on rbserver1:/opt/uptime-kuma (healthy); awaiting user's first-run setup (admin, alert channel, monitors)
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-kuma switched to maintained :2 (v1 EOL); empty v1 DB backed up + migrated; v1 image removed
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-kuma → :2-slim (867MB vs 2.52GB); SQLite + HTTP/DNS monitors need neither MariaDB nor Chromium
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-kuma removed (867MB); decision reversed by user: build own lightweight prober (Go stdlib, alpine)
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-wisp built (Go stdlib, 16.8MB alpine image) + deploy/uptime; found EphemNet ns1/ns2.mera.network NXDOMAIN bug
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-wisp deployed on rbserver1:/opt/uptime-wisp; 14 checks up; ntfy test alert confirmed; EphemNet fixed the ns1 NXDOMAIN finding
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-wisp status page behind required Basic Auth (sha256 password); -check-config pre-flight
2026-10-01 | P001 | IN_PROGRESS → IN_PROGRESS | uptime-wisp: Reload config button + SIGHUP (validated, state-preserving, between rounds); config moved to mounted config/ dir
2026-10-02 | P001 | IN_PROGRESS → IN_PROGRESS | Dashboard: single row of views (Issues, Last day so far, Last day, Last 3/7/30/90 days), products below; History tab removed
2026-10-02 | D002 | PLANNING → PLANNING | Day-close grace 2h → 16 min (user); hook.DayCloseAfter = 24h16m
2026-10-02 | P001 | IN_PROGRESS → IN_PROGRESS | bh2 disk filled by wisp's on-server Go build (killed, recovered); deploy now cross-compiles locally, runtime-only image, prune-on-failure, free-space guard
2026-10-02 | P001 | IN_PROGRESS → IN_PROGRESS | wisp deploy ships a locally built image (docker save|load); server never builds; rollback via kept wisp:<commit> images
2026-10-02 | P001 | IN_PROGRESS → IN_PROGRESS | Image-shipping deploy pattern filed in cinder A024, EphemNet A007, persona A011, offgrid A013; bh2 disk growth traced to cinder's golang:1.24-bookworm (1.27GB)
