# Template Update Propagation

## How It Works

When a `CoreTemplate`, `CellTemplate`, or `ShardTemplate` is updated, the `MultigresCluster` controller enqueues all clusters that reference the changed template for reconciliation. The filtering uses `status.resolvedTemplates` — a status field that records which templates each cluster resolved during its last reconciliation. Clusters that have never been reconciled (nil status) are always enqueued as a safe default.

Template changes are applied **immediately** to all referencing clusters.

## The Problem

Immediate propagation does not scale well. If 100 clusters reference a shared `ShardTemplate` and someone edits it, all 100 clusters are reconciled simultaneously. In production, this creates several risks:

- **Thundering herd**: The operator's work queue saturates with reconciliations, potentially delaying unrelated work.
- **Blast radius**: A misconfigured template change rolls out to every cluster at once with no opportunity to validate on a subset first.
- **No owner control**: The cluster owner has no say in when configuration changes are applied — any template editor can trigger a fleet-wide update.

For beta this is acceptable because we want to rapidly test template effects across clusters. For production at scale, the cluster owner should be able to decide when template updates are adopted.

## Proposed Improvement: Update Policy

> [!IMPORTANT]
> **Status: Not Implemented — Proposal Only.** The fields and behavior described in this section do not exist in the current API. This is a design proposal for a future release.

Add a `spec.updatePolicy` field to `MultigresCluster` that controls when template changes are applied:

```yaml
spec:
  updatePolicy:
    templateChanges: "Auto"  # or "Manual" or "Window"
```

| Mode | Behavior |
|:---|:---|
| `Auto` | Current behavior — reconcile immediately on template change. Default for backward compatibility. |
| `Manual` | Template changes are detected but not applied. The cluster status shows `TemplateUpdateAvailable: True` with a summary. The owner triggers the update by setting an annotation (e.g., `multigres.com/apply-template-update: "true"`) or bumping a spec field. |
| `Window` | Template changes are deferred until a configured maintenance window (e.g., `cron: "0 2 * * SUN"`). |

**How it works:**

1. During reconciliation, the controller compares the template's current `metadata.generation` against the generation stored in `status.appliedTemplateGenerations`.
2. If generations differ and `updatePolicy` is `Manual`, the controller skips the update and sets a status condition:
   ```yaml
   status:
     conditions:
       - type: TemplateUpdateAvailable
         status: "True"
         message: "ShardTemplate 'standard-shard-ha' has pending changes (generation 3 → 5)"
   ```
3. When the user triggers the update (via annotation or spec change), the controller applies the new template configuration and updates the stored generation.

## Considered Alternatives

### Template Version Field + Cluster Version Pinning

A user-defined `version` field on templates, with clusters pinning to a specific version number:

```yaml
# Template
spec:
  version: 3
  # ...

# Cluster
spec:
  templateDefaults:
    shardTemplate: "standard-shard-ha"
    shardTemplateVersion: 3
```

**Not proposed because:**
- It reinvents `metadata.generation` but makes it manual and error-prone.
- If someone edits the template spec without bumping the version, the content silently changes under the same version number.
- Requires users to update two things in sync (template version + cluster version pin).
- Doesn't work with implicit `default` templates (no explicit reference to attach a version to).

### Rolling Wave / Canary Rollout

A `rollout` field on templates controlling propagation order — canary clusters first, then batches with health gates:

```yaml
spec:
  rollout:
    strategy: "Canary"
    maxConcurrent: 5
    canarySelector:
      matchLabels:
        tier: "canary"
    pauseAfterCanary: true
    batchInterval: "5m"
```

**Not proposed because:**
- Very high implementation complexity (internal rollout state machine, health gate logic, batch tracking).
- Only justified for managed-fleet use cases with hundreds of tenant clusters.
- The Update Policy approach (`Manual` / `Window`) covers 90% of the use cases with 10% of the complexity.

### Immutable Template Versioning (Separate Resources)

Creating new template resources for each version (`standard-shard-ha-v1`, `standard-shard-ha-v2`) and updating cluster refs manually.

**Not proposed as an operator feature, but available as a user convention:** This works today through name-versioned templates (documented in the README). However, it puts the rollout responsibility entirely on the user/CI pipeline. The Update Policy approach automates the detection and deferral within the operator itself.
