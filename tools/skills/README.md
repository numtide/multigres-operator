# Operator Development Skills

AI agent skills for operator development workflows. Each skill is a structured prompt that teaches an agent how to perform a specific task with consistency and precision.

## Available Skills

| Skill | Category | Description |
|-------|----------|-------------|
| [generate_commit_message](generate_commit_message/SKILL.md) | Development | Generate semantic Conventional Commits messages from staged changes. |
| [pin_upstream_images](pin_upstream_images/SKILL.md) | Release | Pin multigres container image SHA tags and review upstream code changes between versions. |
| [prepare_release](prepare_release/SKILL.md) | Release | Full release preparation: changelog generation, version inference, and documentation audit. |

## Release Workflow

The release skills are designed to be used together:

```
pin_upstream_images                     prepare_release
───────────────────                     ───────────────
1. Fetch latest SHA tags               1. Gather commits since last tag
2. Update image_defaults.go            2. Categorize changes
3. Review upstream code changes   ──→   3. Update CHANGELOG.md
4. Cross-reference operator impact      4. Audit all documentation
5. Present impact analysis              5. Apply doc fixes
```

**pin_upstream_images** upgrades the multigres container images and reviews what changed upstream. **prepare_release** then prepares the changelog and audits docs for the full release. Both use **generate_commit_message** for their final commit.

## Usage

These skills are loaded automatically by Claude Code when their trigger phrases are used. You can also invoke them directly:

- `/generate_commit_message` -- after staging changes
- `/pin_upstream_images` -- when upgrading multigres images
- `/prepare_release` -- when cutting a new operator release

## See Also

- [Observer Skills](../observer/skills/README.md) -- Skills for cluster health diagnostics (exercise_cluster, diagnose_with_observer)
