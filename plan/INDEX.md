# wisp Plan Index

*Last updated: 2026-10-02 02:54:28 UTC*

Status: `IDEA` · `PLANNING` · `IN_PROGRESS` · `BLOCKED` · `DONE` · `DEFERRED` · `CANCELLED`

---

## Concepts

| ID | Title | Type | Status |
| --- | --- | --- | --- |
| [C001](concepts/C001-no-visitor-side-third-party-requests.md) | No visitor-side third-party requests | constraint | STABLE |

---

## Theses

| ID | Title | Status | Conviction |
| --- | --- | --- | --- |
| [T001](theses/T001-ephemeral-hashing-beats-cookies-for-analytics.md) | A cookieless, salted-hash lower-bound estimate can replace cookie-based visitor tracking for the vast majority of what site owners actually need analytics for | HELD | 6 |

---

## Projects

| ID | Title | Status | Priority | Key Open Work |
| --- | --- | --- | --- | --- |
| [P001](projects/P001-cookieless-analytics-mvp.md) | wisp MVP — cookieless pageview/visitor/download analytics, no consent banner | IN_PROGRESS | MEDIUM | TBD |

---

## Designs

| ID | Title | Status | Project | Doc |
| --- | --- | --- | --- | --- |
| [D001](designs/D001-cookieless-hash-analytics-architecture.md) | Cookieless, ephemeral analytics — collection, hashing, storage, and rollup architecture | PLANNING | P001 | (link if applicable) |
| [D002](designs/D002-ingest-data-model-and-accumulation.md) | Ingest data model — generation, day-scoped staging, and accumulation into the product statistics DB | PLANNING | P001 | (link if applicable) |

---

## Actions

| ID | Title | Status | Design | Open Tasks |
| --- | --- | --- | --- | --- |