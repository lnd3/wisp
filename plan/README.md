# Project Planning System

This directory contains the planning structure for this project, following the **lplan** generic planning framework.

## Framework Reference

The planning system (schema, templates, tools) is defined in the lplan repository, typically added as a git submodule at `deps/lplan/`.

For framework documentation and commands, see:
- `deps/lplan/README.md` — System overview and quick start
- `deps/lplan/QUICK_REFERENCE.md` — Command cheat sheet
- `deps/lplan/schema/` — Formal specifications (frontmatter, project/design/action structure, priority framework)
- `deps/lplan/VALIDATION.md` — Validation workflow and common errors

## Plan Structure

### Key Files
- **INDEX.md** — Master dashboard showing all projects, designs, and actions
- **CHANGELOG.md** — Append-only log of all status and priority changes
- **VALIDATION.md** — Validation requirement and pre-commit workflow

### Directories
- **concepts/** — Stable named abstractions: modes, terms, constraints, rules (C001, etc.)
- **theses/** — Falsifiable beliefs about the world (T001, etc.)
- **master_plans/** — Constrained possibilities seeded by theses (M001, etc.)
- **projects/** — High-level goals and work scope (P001, P002, etc.)
- **designs/** — Architectural specifications and decisions (D001, D002, etc.)
- **actions/** — Concrete implementation task lists (A001, A002, etc.)

## Quick Start

### View Current Status
```bash
cat plan/INDEX.md
```

### Validate Plan Before Commit
```bash
./deps/lplan/bin/plan validate ./plan
```

### Check Dependencies
```bash
./deps/lplan/bin/plan deps P001 ./plan
```

### Analyze Full Dependency Graph
```bash
./deps/lplan/bin/plan graph-report ./plan
```

## Workflow

1. **Edit a plan file** (project, design, action, or INDEX)
2. **Validate** before committing: `./deps/lplan/bin/plan validate ./plan`
3. **Fix any errors** (see VALIDATION.md for common errors)
4. **Re-validate** to confirm pass
5. **Commit** only after validation passes

Optional: add git pre-commit hook for automatic validation (see VALIDATION.md for instructions).

## Priority Framework

Projects are scored using drivers defined in `deps/lplan/schema/priority-framework.md`:
- Score ≥ 2.0 = HIGH priority
- Score 1.0–1.9 = MEDIUM priority
- Score < 1.0 = LOW priority
- Score < 0 = BLOCKED

See the full schema for driver definitions and scoring rules.

## Updates

To update the planning framework:
```bash
cd deps/lplan
git pull origin main
cd ..
git add deps/lplan && git commit -m "Update lplan framework"
```

See lplan repository for release notes and changelog.
