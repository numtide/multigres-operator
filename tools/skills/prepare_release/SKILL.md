---
name: prepare_release
description: Prepare a release by analyzing all changes since the last git tag, updating CHANGELOG.md with categorized entries, inferring the next semantic version, and auditing all documentation for staleness or missing content. Triggered by requests like "prepare release", "bump version", "update changelog", "release prep", "version bump", or "prepare changelog".
---

# Prepare Release

Prepare a release by gathering all changes since the last tag, updating the changelog, and auditing documentation.

## Workflow Overview

1. **Gather changes** -- Identify last tag, collect commits and diffs
2. **Update CHANGELOG.md** -- Categorize changes, infer version, write entry
3. **Audit documentation** -- Check every doc file for staleness
4. **Apply doc updates** -- Fix identified issues

## Phase 1: Gather Changes

1. Identify the last git tag:
   ```bash
   git describe --tags --abbrev=0
   ```

2. Collect all commits between that tag and HEAD:
   ```bash
   git log --oneline <last-tag>..HEAD
   ```

3. Get the diff stats:
   ```bash
   git diff --stat <last-tag>..HEAD
   ```

4. For each commit, read the full commit message and the diff to understand the actual change -- commit subjects alone are insufficient. Use `git show <sha>` for commits that look significant. Pay particular attention to:
   - Changes to `api/v1alpha1/` (CRD changes, potential breaking changes)
   - New files (potential new features)
   - Deleted files (potential breaking changes or cleanup)
   - Changes to container args, env vars, probes (behavioral changes)
   - Changes to `pkg/webhook/` (admission logic changes)
   - Changes to `config/` (RBAC, CRD schema, monitoring rules)

5. Build a categorized list of changes. See [changelog-format.md](references/changelog-format.md) for the section ordering and formatting rules.

6. **Separate observer changes from operator changes.** The observer (`tools/observer/`) is a standalone diagnostic tool, not part of the operator itself. When categorizing changes:
   - Changes under `tools/observer/` (code, docs, skills, fixtures) go into a separate **Observer** subsection at the end of the changelog entry
   - Only operator-level changes (CRD, controllers, webhook, config, pkg/) are listed in the main sections (Features, Bug Fixes, etc.)
   - Observer changes must NOT influence version inference -- see Phase 2

## Phase 2: Update CHANGELOG.md

1. Read [changelog-format.md](references/changelog-format.md) for the exact format conventions.

2. **Infer the next version** from **operator-only** changes (exclude observer). See the detailed rules in [changelog-format.md](references/changelog-format.md), but the key litmus test is:
   - Does this release give users a **new knob they can turn**, a **new resource they can create**, or a **new operational workflow** they couldn't do before? -> MINOR
   - Does it only make existing behavior more reliable, correct, validated, or hardened? -> PATCH

3. Write the new changelog entry at the top of the file (after the header, before the previous version). Include:
   - Version and date header
   - Previous release reference
   - One-paragraph summary of the release theme
   - Stats line (commit count, files changed, insertions)
   - Categorized change sections (omit empty sections)

4. **Distinguish Improvements from Bug Fixes.** Changes that enhance existing behavior -- performance gains, reliability hardening (e.g., adding health probes), validation tightening, operational polish -- go into `### Improvements`, NOT `### Bug Fixes`. Reserve Bug Fixes for things that were genuinely broken. See [changelog-format.md](references/changelog-format.md) for section ordering.

5. Proceed directly to Phase 3 without stopping. If the user wants to adjust the version or changelog entry, that is a trivial change afterwards.

## Phase 3: Audit Documentation

This is the most important phase. Read [doc-inventory.md](references/doc-inventory.md) for the complete file inventory.

### 3a. Change-Driven Audit

For each code change identified in Phase 1, cross-reference the "Related Code" column in the doc inventory to identify which docs might need updating. For each affected doc:

1. Read the doc file
2. Check if the documented behavior still matches the code after the changes
3. Flag any stale content, missing references to new features, or incorrect descriptions

### 3b. Full Staleness Scan

Independently of the code changes, scan every documentation file listed in the inventory for general staleness. For each file:

1. Read the doc file
2. Spot-check key claims against the current codebase (e.g., file paths, function names, CRD fields, controller names, command examples)
3. Flag anything outdated, even if unrelated to the changes since the last tag

Common staleness patterns to look for:
- References to deleted files or renamed packages
- CRD fields that were added/removed but not reflected in docs
- Makefile targets that changed
- Controller behavior that evolved (phases, conditions, events)
- Container args or env vars that changed
- Alert rules or metrics that were added/removed/renamed
- Sample YAML that no longer matches the current CRD schema

### 3c. Report Findings

Present a summary table to the user:

| File | Status | Action Needed |
|:---|:---|:---|
| `README.md` | Current | -- |
| `docs/storage.md` | Stale | Section on PVC deletion references old behavior |
| `docs/development/phase-lifecycle.md` | Missing content | New degraded phase not documented |

Include both change-driven findings (3a) and staleness findings (3b) in the table.

## Phase 4: Apply Documentation Updates

1. For each item flagged in Phase 3, make the fix:
   - Update stale content to match current code
   - Add missing documentation for new features
   - Remove references to deleted features
   - Fix incorrect file paths, function names, or examples

2. Present a summary of all documentation changes to the user.

## Important Rules

- Do NOT commit or stage any files. The user will review and commit manually.
- Do NOT modify `CLAUDE.md` or anything under `agent-docs/` -- these are not part of the project docs.
- Do NOT skip Phase 3b (full staleness scan). Even if no code changed, docs can drift.
- When in doubt about whether a doc is stale, read the actual source code to verify.
- Clean up after yourself -- do not leave scratch files or temporary outputs.
