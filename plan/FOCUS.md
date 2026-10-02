# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **wisp is live and counting** at https://wisp.mera.network (bh2).
  - **Reporting:** five products (cinderapps, offgridapp, eph-network,
    persona, wisp). The first closed day is 2026-10-01 (86 visitors).
  - **Dashboard views:** Issues · Last day so far (default) · Last day ·
    Last 3/7/30/90 days.
  - **Days close** 16 minutes after midnight UTC.
  - **Deploys** build the image locally and ship it with
    `docker save | docker load`; bh2 never builds.
- **uptime-wisp is live** on rbserver1
  (`http://rbserver1.lan:8080`, Basic Auth): 15 checks, alerts to
  ntfy `lnd_bh2_alerts_84af2f`. Edit
  `/opt/uptime-wisp/config/config.json`, then press "Reload config".

---

## Blocked

- Nothing blocking. The products' own deploy changes (image shipping)
  live in their repos and depend on their sessions.

- **Deferred:** the uptime-wisp heartbeat (the user checks the status
  page personally). `heartbeat.url` is ready if that changes.

---

## Next

Nothing scheduled. Deferred by the user (2026-10-02):
- **Image-shipping deploys in the other products** (cinder A024,
  EphemNet A007, persona A011, offgrid A013). Filed; each repo picks it
  up when it's ready. Until cinder's lands, its next server-side build
  re-pulls the 1.27 GB `golang:1.24-bookworm`.
- **Persisting the Issues log** across restarts.
- **D002 leftovers:** confirm the proposed numbers; country/GeoIP.

Done: bh2 housekeeping. cinder's stale `deploy-*`/`dev-*` images had
already been removed by the time we checked; bh2 is at 60%.
