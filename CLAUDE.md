# wisp

Cookieless, ephemeral web analytics — see `README.md` and
`plan/theses/T001-*.md` for the full picture. The one non-negotiable
constraint governing every design decision here: **nothing in this
project may require a cookie-consent popup.** Not "minimize tracking,"
not "make the banner smaller" — no mechanism that legally triggers one
is in scope at all, full stop. See `plan/designs/D001-*.md` for what
that rules in and out concretely.

## Validate before committing

```bash
./deps/lplan/bin/plan validate ./plan
./deps/lplan/bin/plan generate-index ./plan
```

## The core discipline this project runs on

- **No raw IP persists anywhere, ever, under any configuration.** It
  exists only transiently in the request handler long enough to
  compute the day's visitor hash, then is discarded. This is a hard
  invariant, not a default — treat any code path that logs, stores, or
  forwards a raw IP as a bug, not a missed optimization.
- **The daily salt must never be derivable from what gets persisted.**
  If a day's stored events could ever be paired back up with that
  day's raw salt, the "cannot re-identify a visitor later, even by the
  operator" claim becomes false. Generate fresh per rotation window,
  discard after.
- **Unique visitors are a deliberate lower bound, not an approximation
  of an exact count.** Don't add mechanisms later that quietly chase
  more precision at the cost of the privacy properties above — that's
  a different product with the same name, not an improvement.
- **No cookies, no `localStorage`/`sessionStorage` identifiers, no
  cross-site identifiers.** If a feature seems to need one, the answer
  is "that feature is out of scope," not "find a technically-different
  storage mechanism that has the same effect."

## Relationship to cinder / EphemNet / persona

No runtime or architectural dependency on any of them — this is a
standalone project. The kinship is philosophical only: the same
identity-free-by-design instinct (see `cinder`'s own T005) applied to
web analytics instead of messaging/hosting/identity. Don't add a
dependency on any of those repos just because the philosophy rhymes.

## Origin

This repo's idea was previously tracked only in `superplan` (project
`P012`, formed 2026-09-30 from a user conversation about what a web
analytics tool needs). This repo's own `plan/` is now the source of
truth for anything wisp-specific going forward — see `superplan`'s
P012 only for the earlier design-formation history (including the
options considered and rejected before landing on the cookieless-hash
approach).
