# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **wisp is live and counting** at https://wisp.mera.network (bh2).
  - **Registry:** seven products (cinderapps, cindernote, vigil,
    offgridapp, eph-network, persona, wisp). cinderapps reports from bh3
    (cinder A026). The retired cindertransfer and cinderchat were removed
    on 2026-10-12; they had never sent data.
  - **Landing page** links to the source on GitHub.
  - **Dashboard views:** Issues · Last day so far (default) · Last day ·
    Last 3/7/30/90 days. Days close 16 minutes after midnight UTC.
  - **Deploys** build the image locally and ship it
    (`docker save | docker load`); servers never build.
  - **Go:** everything builds with go1.26.10 (A002): `go.mod` is
    `go 1.25.0` + `toolchain go1.26.10`, the Dockerfile is pinned, and
    the health/uptime deploy scripts refuse an older Go. The dev
    machine's PATH is on `~/sdk/go1.26.10` (go1.23.4 deleted).
- **uptime-wisp is live** on rbserver1 (`http://rbserver1.lan:8080`,
  Basic Auth): 16 checks, alerts to ntfy (topic only in the host config).
  Edit `/opt/uptime-wisp/config/config.json`, then press "Reload config"
  or send SIGHUP.
  - **After a power cut** Docker may leave it "Exited (255)" despite
    `unless-stopped` (2026-10-21): `cd /opt/uptime-wisp && docker
    compose up -d`.
  - **rbserver1 deploys** need both an `ssh` and an `scp` wrapper on
    PATH that add `-o HostName=rbserver1.lan -o HostKeyAlias=rbserver1`
    (scp calls /usr/bin/ssh directly).
  - **Connectivity gate (A001, done):** when all three independent
    anchors fail, rounds judge nothing and no alerts go out.
  - **Grouping:** three or more simultaneous changes arrive as one
    alert.
- **health-wisp is live** on three machines, each with one "host" row
  in uptime-wisp:
  - **bh2:** `host-bh2-4637.mera.network` (TLS)
  - **bh3:** `host-bh3-4637.mera.network` (TLS)
  - **rbserver1:** LAN `:8082`

  Tokens are in `/opt/health-wisp/.health-token` on each host. Deploy
  with `deploy/health/deploy.sh`.
- **bh3** (cinder's host now, plus the user's pruned bitcoind) has
  cinder's shared nginx stream skeleton and ufw (22/80/443, logging
  off). ufw logging is off on bh2 too, and its old ufw logs were
  deleted.

---

## Blocked

- Nothing blocking.
- **Deferred:** the uptime-wisp heartbeat (the user checks the status
  page personally). `heartbeat.url` is ready if that changes. Night-time
  connectivity drops on rbserver1 are the user cutting the internet,
  not a fault.

---

## Next

- **ntfy topic rotation (user's call):** the alert topic was public in
  plan files since 2026-10-01 (A003 correction). It's removed from the
  tree but stays in history. A new topic means the user re-subscribes
  on the phone; then swap it in rbserver1's config.json and reload.

Otherwise nothing scheduled. Deferred by the user (2026-10-02):
- **Image-shipping deploys in the other products** (cinder A024,
  EphemNet A007, persona A011, offgrid A013), filed in their repos.
- **Persisting the Issues log** across restarts.
- **D002 leftovers:** confirm the proposed numbers; country/GeoIP.

Watch:
- **Disks:** bh3 72% (cinder plus bitcoind), bh2 69% on 2026-10-22.
  The host checks alert above 85%.
- **Go patch releases:** bump go.mod's toolchain line and the
  Dockerfile together; re-run govulncheck.
