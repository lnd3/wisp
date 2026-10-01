# uptime-wisp: external uptime monitoring

wisp's own lightweight prober (P001's health-monitoring item), which
replaced uptime-kuma, whose slim image was 867 MB. **uptime-wisp's image
is ~17 MB**: alpine, CA certificates and one static Go binary that uses
only the standard library (`cmd/uptime-wisp`, package `uptime`).

## What it does

Every interval (default 60s), from outside the machines it watches:
- **HTTP(S) checks.** GET a public URL and expect a status (default
  200; redirects are reported, not followed). The prober resolves the
  name itself through `resolver` (default 1.1.1.1), never `/etc/hosts`
  or Docker's DNS, so "the domain resolves" is part of the check.
  TLS is verified, with a warning `cert_warn_days` (14) before the
  certificate expires.
- **DNS checks.** Ask a specific nameserver for a host's A/AAAA records
  and compare with `expect`.
- **Alerts only on change:**
  - DOWN after `failures_before_alert` (2) failures in a row
  - "back up", with how long it was down
  - certificate expiring soon, once per certificate

  Channels: **ntfy** (topic URL) and/or a **generic webhook** (JSON).
  Both are third-party, independent of bh2 and rbserver1.
- **Optional `heartbeat`.** A GET after every round, for a third-party
  dead-man's switch such as healthchecks.io. It alerts if the pings
  stop, which covers this host dying, including offgrid's blind spot on
  rbserver1.
- **A status page** on `:8080` (refreshes every 30s) behind **HTTP
  Basic Auth**, which is required: the service refuses to start the page
  without it. `/healthz` stays open; it only says "ok", and Docker uses
  it as the container health check.

## Deploy / update

The host needs Docker with the Compose v2 plugin, `rsync`, and the SSH
user in the `docker` group. No Go toolchain: the binary is
cross-compiled here, for the host's architecture.

```bash
deploy/uptime/deploy.sh rbserver1 /opt/uptime-wisp            # status page on :8080
deploy/uptime/deploy.sh rbserver1 /opt/uptime-wisp --port=8090
```

1. **First run:** it stops and asks you to create `config/config.json`
   on the host from the shipped `config.example.json`. That file holds the
   alert secret, such as the ntfy topic, so it's never synced or
   committed.
2. **Every run:** it validates the host's config with the new binary,
   exactly as the service will run it (`-check-config`), *before*
   replacing the running one, so a config the new version
   rejects can't take monitoring down. It then rebuilds the tiny image
   and restarts.
3. **Port and bind** are remembered on the host. A port that something
   else already uses is refused.

**Changing checks (no deploy needed):** edit
`/opt/uptime-wisp/config/config.json` on the host with any editor, then
press **Reload config** on the status page. It's re-read and validated
exactly as at startup.
- **An invalid file is rejected,** the running config is kept, and the
  error is shown on the page.
- **Unchanged checks keep their state,** so a reload never sends
  spurious alerts. New checks start "unknown" and are checked
  immediately; removed ones disappear.
- **Login and alert-channel changes** apply at once.
- **Changing `listen`** needs a restart.

From a shell, `docker compose -p uptime-wisp kill -s HUP uptime-wisp`
does the same.

**Prove alerts reach you** (do this once after setup):

```bash
ssh rbserver1 'cd /opt/uptime-wisp && docker compose -p uptime-wisp exec uptime-wisp uptime-wisp -config /etc/uptime-wisp/config.json -test-alert'
```

**Status page login:** `config.json`'s `auth` holds the user and the
*SHA-256* of a long random password, never the password itself. On
rbserver1 the password is in `/opt/uptime-wisp/.status-password`
(0600). To set a new one:

```bash
PW=$(openssl rand -base64 24)
printf %s "$PW" | sha256sum | cut -d' ' -f1   # → auth.password_sha256 (no trailing newline!)
```

(Or use `uptime-wisp -hash-password`, which reads the password from
stdin and handles the newline.) Edit `config/config.json` (keep the rest of the file), then press
**Reload config**. The old password stops working at once. Basic Auth over
plain HTTP is fine on a home LAN, but the password crosses the network
unencrypted, so don't expose this port to the internet.

**Dry run a config** without alerting (exit 1 if any check fails,
exit 2 if the config is invalid):
`uptime-wisp -config config.json -once`

## Config

See `config.example.json`, which already lists every product's public
URL. Fields:

| Field | Default | Meaning |
|---|---|---|
| `interval` | `60s` | time between rounds (min 5s) |
| `timeout` | `10s` | per check; must be shorter than `interval` |
| `failures_before_alert` | `2` | consecutive failures before DOWN |
| `cert_warn_days` | `14` | warn when a certificate expires sooner |
| `resolver` | `1.1.1.1:53` | DNS server for HTTP checks' own lookups |
| `heartbeat.url` | — | optional dead-man's-switch ping |
| `auth` | required with the status page | `{"user":…, "password_sha256":…}` |
| `alerts[]` | required | `{"type":"ntfy"\|"webhook","url":…,"token":…}` |
| `checks[]` | required | `http`: `url`, `expect_status`; `dns`: `host`, `server`, `record_type` (A/AAAA), `expect` |

Unknown keys are rejected, so a typo fails loudly instead of being
silently ignored.

**ntfy setup:** install the ntfy app, subscribe to a long random topic
(`openssl rand -hex 16`) on ntfy.sh, and put
`https://ntfy.sh/<that-topic>` in `alerts`. On ntfy.sh, a topic is
secret only by being unguessable.

## First finding

The prober's first live dry run (2026-10-01) found that
`ns1`/`ns2.mera.network` returned NXDOMAIN publicly. EphemNet's
nameserver answered its own NS hostnames with a synthesized SOA; only
the `.network` glue kept delegation working. EphemNet fixed it the same
day (EphemNet `6b39d4f`). The `ns1.mera.network resolves` check stays
in the config as a regression guard.
