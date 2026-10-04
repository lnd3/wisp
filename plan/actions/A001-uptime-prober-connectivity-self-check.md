---
id: A001
title: uptime-wisp should verify its own connectivity before declaring a target down
status: IN_PROGRESS
project: P001
created: 2026-10-04
updated: 2026-10-04
---

## Why

A real incident surfaced this gap, reported from `EphemNet` (a
downstream product `uptime-wisp` monitors): alarms fired at 01:39 UTC
on 2026-10-04 for what looked like "everything went down, then almost
immediately came back up." Investigated directly on EphemNet's own
relay server (`bh2`) — it never restarted, and neither did the
`ephemnetd` process. The real event: two EphemNet relay tunnel
agents (`uptime.wisp.mera.network`, `demo.offgridapp.mera.network`)
briefly lost their keepalive to the relay at 01:37:35 UTC and
reconnected 82 seconds later, both from the same source IP, while two
other unrelated tunnels (different source IP) stayed connected the
entire time.

Root cause, per the user: both affected machines sit on their own
Starlink connection, the same network `rbserver1` (where `uptime-wisp`
itself runs) is also on. Starlink's own satellite-handoff dropouts are
exactly this shape — brief, self-resolving. When filed, this action
assumed `rbserver1` stayed up through the blip ("The only reason we
know about this, is because rbserver1 on my starlink network, was
up").

**Corrected 2026-10-04 from uptime-wisp's own log on rbserver1: it did
not stay up.** rbserver1's own uplink dropped too, and every alert
that night came from that:
- 01:38:44–01:39:01 UTC: all 19 checks went DOWN with `timeout`. That
  includes targets that share nothing but the prober: DNS queries to
  1.1.1.1 for `ns1`/`ns2.mera.network`, the bh3 host on a different
  IP, and every bh2 site.
- 01:38:56: the first ntfy delivery failed with `lookup ntfy.sh …
  server misbehaving`. The prober couldn't even resolve names.
- 01:39:39–01:39:41: all 19 recovered "after 56s".

That's 38 notifications for one blip of the prober's own link: exactly
the "I am down, not my targets" case this action anticipated, not a
near miss. The two dropped EphemNet agents were the same Starlink blip
seen from bh2's relay, not evidence that rbserver1 was online.

User's own framing, verbatim: "Maybe wisp should not react if it has
no connectivity to some key networks at all."

## What needed deciding/building (resolved below)

- Pick (or build) an independent reference check — e.g. a well-known,
  highly-available external anchor (`1.1.1.1`, `8.8.8.8`, or similar)
  that `uptime-wisp` probes alongside its real targets.
- Decide the suppression rule: if the reference check itself fails
  (or some fraction of all monitored targets fail simultaneously,
  suggesting a local/shared-path problem rather than N independent
  target outages), hold/suppress the alert rather than firing on every
  affected target — surfacing a distinct "my own connectivity is
  degraded" state instead of a wall of false "target down" alerts.
- Decide whether this gates ALL alerting or only alerting for targets
  that share network path/geography with the prober itself (not
  obviously knowable from `rbserver1`'s own vantage point alone).

## Log

2026-10-04 — Filed from a real incident reported out of EphemNet's own
session (see that repo's own `plan/designs/D001-dns-server-reverse-
tunnel-and-tiering.md` Log, 2026-10-04, for the full EphemNet-side
investigation and timeline this is based on).

2026-10-04 — Cause corrected from uptime-wisp's own logs (see Why).
The user approved building the gate. Built in package `uptime`:
- **Anchors** (`connectivity.anchors`): by default DNS
  `cloudflare.com` @1.1.1.1, DNS `quad9.net` @9.9.9.9, and HTTPS
  `www.google.com/generate_204`, three providers sharing no
  infrastructure. They're probed concurrently with the checks every
  round. Any http/dns check can be an anchor; `{"disabled":true}`
  turns the gate off.
- **Suppression rule:** a round in which *every* anchor fails judges
  nothing. No failure is counted (counts freeze, they don't reset), no
  alert is sent, and the status page shows "uptime-wisp itself is
  OFFLINE … checks are paused".
- **Long outages:** once an anchor answers again, an outage of at
  least `report_after` (default 5m) is reported in one "uptime-wisp
  was offline for …" alert. Shorter blips just log.
- **Path-specific gating** (the third question): not needed. Whether
  a target shares the prober's path isn't knowable from rbserver1, and
  "all anchors down" already separates the prober's own uplink from
  real outages.
- **Grouping:** three or more checks changing the same way in one
  round, with the anchors fine, arrive as one alert ("4 checks DOWN",
  one line each), so a real shared-cause outage, e.g. bh2 dying, isn't
  a wall of notifications either.
- **`-once`:** prints the anchors and exits 3 when all of them failed.

Replay test: `TestOfflineRoundsJudgeNothing` (01:38's shape: two
offline rounds, all failing) now sends zero alerts. It used to send
38.
