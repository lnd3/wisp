---
id: C001
title: No visitor-side third-party requests
type: constraint
status: STABLE
created: 2026-09-30
updated: 2026-09-30
related:
  - D001
  - D002
  - T001
---

## Definition

A page served by any product in the portfolio must never cause the
visitor's browser to request a third-party origin for analytics. That
rules out scripts, tracking pixels, beacons, `fetch`/`sendBeacon`
calls and embedded resources pointing at an analytics host, wisp's
own `wisp.mera.network` included. Analytics data is collected on the
product's own server, from requests the visitor already chose to
make, and forwarded server-to-server. Forcing visitors to contact a
third party without their consent is considered simply offensive, not
merely a privacy cost to minimise.

## Properties

| Property | Value |
|----------|-------|
| Scope | every product that reports to wisp, portfolio-wide |
| Collection point | the product's backend request handler (wisp's `hook`, [[D002]] §1b) |
| Transport | product server → wisp, authenticated per product |
| Visitor-visible effect | none: no extra requests, no extra origins, nothing to consent to |

## Constraints

- **The ingest API must stay server-to-server.** wisp never offers a
  browser-callable ingest path: no CORS allowance for product
  origins, no pixel or beacon endpoint, no client snippet. A product
  embedding any wisp URL in a visitor-facing page violates this rule.
- **It also limits what can be measured.** Anything only a browser
  can observe (client-side SPA route changes, scroll depth, clicks
  that don't hit the server, screen size) is out of scope. It isn't
  worked around with a first-party proxy script either: the visitor
  would still be running analytics code they never asked for.
- **Not a loophole for fingerprinting.** Collecting server-side
  doesn't license widening what's collected. [[D001]]'s lower-bound
  and no-raw-IP rules still apply to the product-side hook.

## When to Use

Apply it to any feature request touching visitor-side behaviour. If
the feature needs the visitor's browser to contact anything beyond
the product's own origin for measurement, it's out of scope. The same
goes for running measurement code in their browser.

## Related

- [[D001]]: the architecture revision this rule motivated (backend-only ingest)
- [[D002]]: the in-product hook and data model that implement it
- [[T001]]: sharpens its no-consent-banner claim, since visitors never contact wisp

## Log

2026-09-30 — Created from the user's stated rationale for backend-only
ingest: "to avoid sprawling outbound requests at the visitor
endpoint. We consider it being, simply offensive, to force visitors
to make requests towards third-party websites, without their
consent."
