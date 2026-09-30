# wisp

Cookieless, ephemeral web analytics for self-hosted web products —
unique visitors, pageviews, and downloads, without ever triggering a
cookie-consent popup.

## The idea

Most site owners don't need an exact, durable identity for every
visitor — they need a reasonable estimate of how many people showed
up, which pages they read, and which files they downloaded. wisp
computes a daily, rotating-salt hash of `IP + User-Agent` as a
deliberate **lower-bound estimate** of unique visitors: no cookie, no
`localStorage`, no persistent identifier, no raw IP ever stored. A
returning visitor the next day looks like a new one — by design, not
as a limitation to work around.

See `plan/theses/T001-*.md` for the full reasoning and
`plan/designs/D001-*.md` for the technical design.

## Status

Early — design decisions made, implementation not started. See
`plan/projects/P001-*.md` for current scope and tasks.

## Validate the plan before committing

```bash
./deps/lplan/bin/plan validate ./plan
./deps/lplan/bin/plan generate-index ./plan
```

## License

MIT — see `LICENSE`.
