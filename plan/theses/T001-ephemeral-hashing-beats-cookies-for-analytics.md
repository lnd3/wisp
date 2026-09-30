---
id: T001
title: A cookieless, salted-hash lower-bound estimate can replace cookie-based visitor tracking for the vast majority of what site owners actually need analytics for
status: HELD
conviction: 6
created: 2026-09-30
updated: 2026-09-30
---

## The Belief

Most site owners deploying web analytics don't need an exact, durable,
cross-session identity for every visitor — they need a reasonable
estimate of how many people visited, which pages they went to, and
which files they downloaded. A cookieless daily-rotating-salted-hash
scheme (`hash(IP + User-Agent + a salt that rotates every 24h)`) gives
a real, defensible **lower-bound** estimate of unique visitors without
persistent identifiers, without a cookie, and without triggering
GDPR/ePrivacy consent-banner requirements — and that estimate is good
enough for the overwhelming majority of real decisions site owners
actually make from analytics (is traffic up or down, which pages get
read, which files get downloaded), even though it deliberately cannot
support cross-session user journeys, cohort retention analysis, or
anything requiring a durable per-person identity. The trade is
correct, not merely acceptable: precision that requires tracking
people is precision most site owners don't need and shouldn't be
paying a privacy cost for.

## Why This Could Be True

- Prior art already validates the core mechanism in production:
  GoatCounter, Plausible, Fathom, and Simple Analytics all ship
  cookieless visitor counting as their default or only mode, and are
  real, paying products — the market has already shown this is
  "enough" analytics for a large customer base, not just a theoretical
  compromise.
- The EU's own ePrivacy guidance draws the line at persistent
  identifiers used for tracking/profiling, not at counting in general —
  a scheme that provably cannot re-identify a specific person across
  days (because the salt rotates and is discarded) sits outside that
  line by construction, not by aggressive interpretation.
- Site owners overwhelmingly report traffic in round numbers and
  trends ("visits are up 20% this month," "this post got 3x the reads
  of the last one") — the kind of question a lower-bound daily-hash
  estimate answers just as well as an exact cookie-based count, since
  both numbers move together even if their absolute values differ.

## What Would Change My Mind

- If a real deployment shows the lower-bound estimate diverging so
  unpredictably from actual traffic (e.g. because of carrier-grade NAT
  concentrating huge numbers of real users behind few IPs) that trends
  themselves become unreliable, not just the absolute count — that
  would mean the estimate fails at the one job this thesis claims it's
  good enough for.
- If real users of a tool built this way consistently ask for exactly
  the durable-identity features this design refuses to build (cross-
  session funnels, retention cohorts) and treat their absence as a
  dealbreaker rather than an acceptable trade — that would mean the
  target market overlaps less with "doesn't need tracking" than assumed.
- If regulatory guidance narrows further to treat even a rotating,
  non-reversible daily hash as regulated tracking regardless of
  reversibility — the "no consent banner needed" claim would be false,
  removing the design's main practical advantage over cookie-based
  alternatives.

## Entropic Constraints

- **Decay mechanism**: regulatory reinterpretation (see above) is the
  main risk; a slower, secondary risk is carrier/NAT IP-sharing
  becoming so common (mobile carriers, corporate networks) that the
  hash's IP component stops meaningfully distinguishing visitors at all.
- **Horizon**: slow — this isn't a competitive/market-timing thesis,
  it's a claim about what's technically and legally sufficient, which
  doesn't have an obvious expiry the way a market-window thesis would.
- **Early warning signs**: EU regulatory guidance specifically
  addressing rotating/salted hashing (not just cookies) would be the
  clearest signal to watch for.
- **What comes after**: if this fails on the NAT/IP-sharing axis
  specifically, a client-side-only signal (canvas/audio fingerprint,
  itself a heavier privacy trade-off) would be the likely successor —
  but that trades away the "we use only what the browser already
  hands over passively" property this design is built around, so it
  isn't a drop-in replacement, it's a genuinely different, worse trade.

## Master Plans Seeded by This

- None yet — this repo is starting directly from a project ([[P001]]),
  not a master plan, matching the scale of the idea at this stage.

## Log

2026-09-30 — Thesis formed at repo creation, seeding this repo from
`superplan`'s `P012` (ephemeral cookieless web analytics idea,
originally captured 2026-09-30 from a user conversation exploring what
a web analytics tool needs to track unique visitors, pageviews, and
downloads). The user's own explicit, non-negotiable constraint — no
cookie-consent popup, ever, cookieless daily-hash approach, deliberate
lower-bound estimate rather than an exact count — is the thesis itself,
not an implementation detail layered on top of a more conventional
analytics design.

2026-09-30 (later) — Checked against D001's backend-only-ingest
revision: the belief itself is unchanged (a daily-salted lower bound
is enough), and it arguably strengthens the no-consent-banner claim,
since no browser ever contacts wisp. What shifts is *where* the hash
runs, not whether it's sufficient. Conviction unchanged at 6.

