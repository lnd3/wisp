---
id: A003
title: Sensitive-information audit of the public repo
status: DONE
project: P001
created: 2026-10-22
updated: 2026-10-22
---

## Why

The user asked for the repo to be checked for sensitive information.
The repo is public on GitHub, and proxy.golang.org mirrors its
versions, so anything committed should be treated as published for
good.

## What was checked

- **gitleaks** (v8.21.2, from its Docker image) over the full history
  (56 commits): no leaks found.
- **Pattern search** over the current files, every past version and the
  commit messages. Patterns: 64-hex strings, private-key headers,
  bcrypt hashes, ntfy topics, Bearer tokens, password/token
  assignments, LAN addresses, usernames.
  - Every hit was a test fixture or placeholder: repeated-character
    test tokens, the SHA-256 of "password" in a test, `ntfy.sh/t`, and
    the example config's `REPLACE-WITH-A-LONG-RANDOM-TOPIC`.
- **Server-only files:** product registry, `.env` files, passwords,
  health tokens and the uptime config were never committed. The
  `.gitignore` covers the two that sit inside the tree; the rest live
  only on the servers.

**Not checked:** the real server-side secrets against the repo, i.e. a
direct comparison of the deployed tokens, passwords and ntfy topic
with the history. Both ways of doing it read the secret files, and
auto mode blocked them as credential materialization. With gitleaks
and the pattern search clean, a match is very unlikely. If it's ever
wanted: run a per-server comparison that prints only a match count.

## Findings: public, not secret (accepted by the user)

1. **Author email:** every locally made commit carries the author's
   personal email address. It's already pushed and mirrored. Rewriting
   history would change every commit hash and break the pseudo-versions
   the products pin in their `go.mod`, and the module proxy keeps the
   old versions anyway, so it stays. Optional for future commits: the
   GitHub noreply address in `git config user.email`.
2. **Server addresses:** bh2's and bh3's public IPs appear in the
   example config, tests, the deploy README and P001. Both already
   resolve publicly through mera.network DNS.
3. **Operational detail in `plan/`:** the Pi's login name, ports,
   Docker subnets and the paths (not the contents) of server-side
   secret files. It's a map of the setup, not access to it.

The user's verdict: "a bit sensitive stuff left, but nothing critical."
Left as is. Keep the same standard when writing plan entries: never
put a secret value in the repo, and keep host-only state in the
gitignored or server-only files.

## Log

2026-10-22 — Audit run and written up; findings accepted as is.
