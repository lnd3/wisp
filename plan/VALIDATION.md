# Plan Validation

This plan uses the [lplan](https://github.com/lnd3/lplan) framework for structured project management.

## Validation Requirement

**Before committing changes to `plan/` directory, always validate:**

```bash
./deps/lplan/bin/plan validate ./plan
```

Expected output:
```
✓ Validation passed (N entities)
(0 warnings)
```

## Common Errors

| Error | Fix |
|-------|-----|
| `depends` is string `P001` | Change to list: `depends: [P001]` |
| `priority: low` | Must be uppercase: `priority: LOW` |
| `priority_drivers` empty list | Must have ≥1 driver |
| Missing `updated:` date | Add: `updated: YYYY-MM-DD` |
| ID format `A006-Phase2` | ID must be exactly `A006`, `P001`, `D003` etc (no hyphens or suffixes) |
| Invalid `priority_drivers` item | Check lplan schema for valid drivers |

## Schema Reference

Full schema documentation in `deps/lplan/schema/`:
- **frontmatter.md** — YAML structure and required fields
- **project.schema.md** — Valid fields for project files
- **design.schema.md** — Valid fields for design files
- **action.schema.md** — Valid fields for action files
- **index.schema.md** — INDEX.md structure rules
- **priority-framework.md** — Valid drivers and scoring

## Workflow

1. **Edit plan file** (project, design, action, or INDEX)
2. **Validate** before commit: `./deps/lplan/bin/plan validate ./plan`
3. **Fix errors** if any (see Common Errors table above)
4. **Re-validate** to confirm pass
5. **Commit** only after validation passes

## Pre-Commit Hook (Optional)

To automate validation, add to `.git/hooks/pre-commit`:

```bash
#!/bin/bash
if git diff --cached --name-only | grep -q '^plan/'; then
    ./deps/lplan/bin/plan validate ./plan || exit 1
fi
```

Make executable: `chmod +x .git/hooks/pre-commit`

## lplan Commands

- `plan validate ./plan` — Check all files for errors
- `plan priority ./plan` — Compute project priorities
- `plan deps P001 ./plan` — Show dependencies
- `plan graph-report ./plan` — Full dependency analysis
- `plan check-refs ./plan` — Find broken links

See `deps/lplan/README.md` and `deps/lplan/QUICK_REFERENCE.md` for complete documentation.

## Troubleshooting

Detailed help available in:
- **Quick answers**: `deps/lplan/QUICK_REFERENCE.md`
- **Troubleshooting**: `deps/lplan/TROUBLESHOOTING.md`
- **Full documentation**: `deps/lplan/IMPLEMENTATION.md`
- **Architecture**: `deps/lplan/README.md`
