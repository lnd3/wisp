---
id: A001
title: uptime-wisp should verify its own connectivity before declaring a target down
status: IDEA
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
exactly this shape — brief, self-resolving. The only reason the blip
was even noticed at all is that `rbserver1` happened to stay up
through it: **"The only reason we know about this, is because
rbserver1 on my starlink network, was up."** Had `rbserver1`'s own
uplink blipped at the same moment (a real possibility on a shared
connection), `uptime-wisp` would have seen every target on that path
fail simultaneously and had no way to tell "my targets are down" apart
from "I am down" — a false-negative-prone alarm, not a confirmed one.

User's own framing, verbatim: "Maybe wisp should not react if it has
no connectivity to some key networks at all."

## What needs deciding/building

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
