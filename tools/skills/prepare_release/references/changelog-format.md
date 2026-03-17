# CHANGELOG.md Format Reference

## Structure

The changelog follows [Keep a Changelog](https://keepachangelog.com/) conventions with project-specific additions.

### Entry Template

```markdown
## [vX.Y.Z] -- YYYY-MM-DD

**Previous release:** vA.B.C (YYYY-MM-DD)

One-paragraph summary of the release -- what it introduces, changes, or fixes at a high level.

**N commits, M files changed, ~K insertions.**

### Breaking Changes
(Only if applicable. Describe what breaks and migration steps.)

### Features
- **Bold label:** Description of the feature.

### Improvements
- **Bold label:** Enhancements to existing behavior -- performance gains, reliability
  hardening, probe additions, validation tightening, or operational polish that
  don't add new user-facing knobs.

### Bug Fixes
- **Bold label:** Description of the fix.

### Dependencies
- Upgrade/downgrade notices for container images or Go modules.

### Refactoring
- Internal structural changes not visible to users.

### Testing
- Test infrastructure or coverage changes.

### Documentation
- New or updated documentation files.

### Observer
- Changes to the observer tool (`tools/observer/`), including new checks,
  endpoints, docs, skills, and fixtures. Listed separately because the
  observer is a standalone diagnostic tool, not part of the operator.
```

## Rules

1. Sections appear in the order: Breaking Changes -> Features -> Improvements -> Bug Fixes -> Dependencies -> Refactoring -> Testing -> Documentation -> Observer. Omit empty sections.
2. Each bullet uses **bold label** prefix followed by a description.
3. The entry is separated from the previous version by a horizontal rule (`---`).
4. The summary paragraph should capture the release theme in 1-2 sentences.
5. The stats line (`N commits, M files changed`) is derived from `git log --oneline` count and `git diff --stat` summary.
6. Date format: `YYYY-MM-DD`.

## Version Inference Rules

Use semantic versioning (MAJOR.MINOR.PATCH) based on **operator-only changes** (exclude observer):

- **MAJOR bump (vX.0.0)**: Breaking API changes (CRD field removals, behavioral changes that require user action, removed controllers).
- **MINOR bump (vX.Y.0)**: Genuinely new user-facing operator capabilities -- new CRD fields that unlock new functionality (e.g., new `DurabilityPolicy` field), new controllers, new operational modes, or new API surface area that users can configure.
- **PATCH bump (vX.Y.Z)**: Bug fixes, improvements (performance, hardening, validation tightening), dependency updates, documentation, refactoring, test changes -- anything that doesn't add new user-facing operator capabilities.

### What does NOT qualify as a MINOR bump

The following are PATCH-level changes even though they may touch CRD schemas or add new behavior:

- **Health probe additions/changes** (startup, liveness, readiness probes) -- these are reliability hardening, not new features.
- **Validation constraints** (CEL rules, pattern validation, immutability guards) -- these restrict existing fields rather than adding new capabilities.
- **Status field population fixes** -- filling in empty status fields is a bug fix.
- **Requeue/retry improvements** -- fixing reconciliation timing is a bug fix.
- **CRD pattern-only changes** (adding regex validation to existing string types) -- no new user-facing knobs.
- **Container arg changes** that don't expose new configuration to users.

### When to use MINOR vs PATCH

Ask: "Does this release give users a new knob they can turn, a new resource they can create, or a new operational workflow they couldn't do before?" If yes -> MINOR. If the changes only make existing behavior more reliable, correct, or validated -> PATCH.

When both genuine features and bug fixes are present, use a MINOR bump. When only bug fixes / hardening / docs / deps / refactoring, use a PATCH bump.

> **Observer changes (`tools/observer/`) do NOT influence version inference.** The observer is a standalone diagnostic tool. Even if a release adds major observer features, the version bump is determined solely by operator-level changes.
