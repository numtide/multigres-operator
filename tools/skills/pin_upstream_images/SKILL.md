---
name: pin_upstream_images
description: Pin multigres container image tags in image_defaults.go for operator releases. Compares upstream multigres code changes between the current and new SHA, highlights breaking changes and new features, then updates the tags. Triggered by user requests like "prepare images for release", "pin image tags", "pin upstream images", or "upgrade multigres images".
---

# Pin Upstream Images

Upgrade multigres container image tags in `api/v1alpha1/image_defaults.go` to the latest SHA tags, with an upstream code review between the old and new versions.

## Workflow

### 1. Fetch Latest SHA Tags

Run the fetch script to get the latest SHA tags:

```bash
python3 tools/skills/pin_upstream_images/scripts/fetch_latest_tags.py
```

This outputs `KEY_TAG=sha-XXXXXXX` for each image. The script resolves which `sha-*` tag currently points to the same digest as `main` for each of:
- `ghcr.io/multigres/multigres` -> used by DefaultMultiAdminImage, DefaultMultiOrchImage, DefaultMultiPoolerImage, DefaultMultiGatewayImage
- `ghcr.io/multigres/pgctld` -> used by DefaultPostgresImage
- `ghcr.io/multigres/multiadmin-web` -> used by DefaultMultiAdminWebImage

### 2. Update Image Tags

Immediately update `api/v1alpha1/image_defaults.go` by replacing the old `sha-XXXXXXX` tags with the new ones:

| Constant                   | Image                              | Tag source      |
|----------------------------|------------------------------------|-----------------|
| DefaultPostgresImage       | ghcr.io/multigres/pgctld           | PGCTLD_TAG      |
| DefaultMultiAdminImage     | ghcr.io/multigres/multigres        | MULTIGRES_TAG   |
| DefaultMultiAdminWebImage  | ghcr.io/multigres/multiadmin-web   | MULTIADMIN_WEB_TAG |
| DefaultMultiOrchImage      | ghcr.io/multigres/multigres        | MULTIGRES_TAG   |
| DefaultMultiPoolerImage    | ghcr.io/multigres/multigres        | MULTIGRES_TAG   |
| DefaultMultiGatewayImage   | ghcr.io/multigres/multigres        | MULTIGRES_TAG   |

Do NOT modify DefaultEtcdImage -- it uses a separate versioned release.

If old and new SHAs are identical for all images, inform the user that images are already up to date and stop.

### 3. Upstream Code Comparison

All multigres images (`multigres`, `pgctld`, `multiadmin-web`) are built from the same monorepo: `https://github.com/multigres/multigres`. Perform the comparison there.

1. Clone or pull the upstream multigres repo to `/tmp/multigres`:
   ```bash
   if [ -d /tmp/multigres ]; then
     cd /tmp/multigres && git fetch --all && git checkout main && git pull
   else
     git clone https://github.com/multigres/multigres /tmp/multigres
   fi
   ```

2. Identify the unique old and new SHA values from Step 2. Since `multigres/multigres` and `multigres/pgctld` may have different SHA tags, compare each distinct old->new pair. Typically:
   - **MULTIGRES_TAG** old SHA vs new SHA (covers multiadmin, multiorch, multipooler, multigateway, and pgctld if they share the same SHA)
   - **MULTIADMIN_WEB_TAG** old SHA vs new SHA (if different from the above)

3. For each distinct old->new SHA pair, run:
   ```bash
   cd /tmp/multigres
   git log --oneline <old-sha>..<new-sha>
   git diff --stat <old-sha>..<new-sha>
   ```
   Then selectively review the full diff for files that look relevant to the operator (e.g., changes to CLI flags, configuration, proto definitions, RPC interfaces, container entrypoints, health checks, pooler behavior, orchestrator logic, gateway behavior, backup/restore, topology management).

4. If the old and new SHAs are the same for an image group, note that no changes occurred and skip the diff.

### 4. Cross-Reference with Operator Code

This is the critical step. Do NOT make hypothetical recommendations. For each upstream change identified in Step 3, **search the operator codebase** to determine concrete impact.

For every potentially impactful upstream change:

1. **Search the operator code** using `grep_search`, `view_file_outline`, and `view_code_item` to find whether the operator references, uses, or depends on the changed upstream construct (proto field, gRPC service, CLI flag, topology record field, behavior, etc.).

2. **Classify each change with evidence:**
   - **Action required** -- The operator code directly references or depends on something that changed. Cite the exact file and line in the operator. Describe what needs to change and why.
   - **New feature opportunity** -- The upstream change introduces a capability the operator *could* support but currently does not. Cite what the upstream change adds and confirm the operator has no existing support for it.
   - **No impact** -- The upstream change does not touch anything the operator interacts with. Confirm this by showing the search came up empty.

3. **Do not speculate.** Every recommendation must be backed by a concrete search result (or confirmed absence) in the operator codebase. No "if the operator does X" phrasing -- either it does or it doesn't.

### 5. Present Results

Present a structured summary to the user:

**Commits between old and new:**
- List of commit messages (from `git log --oneline`)

**Impact Analysis:**
For each upstream change, include:
- What changed upstream (one-liner)
- Operator files affected (with file paths and line references) or "none found"
- Verdict: **Action required** / **New feature opportunity** / **No impact**
- For action-required items: specific description of what needs to change in the operator

### 6. Finalize

1. Display the `image_defaults.go` diff to the user.

2. Suggest a branch name and generate a commit message using the `generate_commit_message` skill:
   - Branch name: `release/pin-image-tags-YYYY-MM-DD`
   - The commit message should reference upgrading multigres images and include the old->new SHA transitions
