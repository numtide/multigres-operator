# Design: DRAINED Pod Handling and PVC Retention

## Terminology

Two concepts share confusingly similar names:

- **`DRAINED`** (the `PoolerType`) — a multigres upstream concept. Set by multiorch in the topology store when a replica's data has diverged and `pg_rewind` fails. Currently the only automated trigger is `markPoolerDrained()` in `fix_replication.go`. The protobuf describes it as "temporarily removed from serving traffic."

- **draining** (the operator state machine) — the operator's internal mechanism for safely removing any pod, using the `multigres.com/drain-state` annotation. This is used for scale-down, rolling updates, external deletions, and DRAINED pod cleanup. During draining, the operator calls `UpdateSynchronousStandbyList(REMOVE)` on the primary to detach the standby. This runs for every pod removal regardless of whether the data is good or bad.

## Background

When multiorch detects a replica whose data has diverged from the primary and `pg_rewind` fails (or is not feasible due to missing WAL), it marks that pooler as `PoolerType_DRAINED` in the multigres topology store. The operator detects this via `resolvePodRole()` which reads from `shard.Status.PodRoles`.

Multigres has no `Decommission` RPC, and the multipooler never deletes its own topology entry on shutdown. These are current upstream limitations that force the operator to manage the full pod lifecycle.

## Current Behavior

The operator implements an annotation-based drain state machine:

```
requested → draining → acknowledged → ready-for-deletion
```

This is triggered in three scenarios:

1. **Scale-down** — `replicasPerCell` reduced, extra pods are drained one at a time
2. **Rolling updates** — drifted pods are drained and recreated (replicas first, primary last with switchover)
3. **DRAINED pooler replacement** — the topology store says a pod is `DRAINED`, the operator immediately initiates drain, deletes the pod, and creates a replacement at the same index

The state machine handles removing the standby from `synchronous_standby_names`, unregistering the multipooler from the topology store, and includes safety guards (primary readiness checks, 5-minute force-timeout for deadlocks).

### PVC Deletion Policies

The operator has two independent PVC deletion policies:

| Policy | Trigger | Mechanism | Default |
|---|---|---|---|
| `WhenScaled` | Pod removal (scale-down, drain cleanup) | Operator explicitly calls `r.Delete(ctx, pvc)` in `cleanupDrainedPod()` | `Retain` |
| `WhenDeleted` | Shard/cluster deletion | ownerRef on PVC → Shard, Kubernetes GC cascades | `Retain` |

Pods never own PVCs. `kubectl delete pod` never triggers automatic PVC deletion through Kubernetes GC. Any PVC deletion after a pod deletion is the operator's code doing it during drain cleanup.

### Current `kubectl delete pod` Behavior

| Pod type | `WhenScaled: Delete` | `WhenScaled: Retain` |
|---|---|---|
| Healthy replica (idx < replicas) | PVC retained (rolling-update path) | PVC retained |
| DRAINED replica (idx < replicas) | PVC deleted (`isDrainedReplacement`) | **PVC retained — causes DRAINED loop** |
| Extra pod (idx >= replicas) | PVC deleted | PVC retained (orphaned) |

## What Breaks

With the current auto-drain behavior and `WhenScaled: Retain`:

1. Operator detects `DRAINED` pod → auto-drains → deletes pod → leaves PVC
2. Replacement pod at same index mounts existing PVC with diverged data
3. Replica tries to join replication → `pg_rewind` fails again → multiorch marks it `DRAINED`
4. Infinite loop

Even with `WhenScaled: Delete`, the operator auto-cleans up DRAINED pods immediately with no mechanism for forensic investigation.

## Proposed Changes

Based on discussion with Sugu, the approach is:

### 1. Keep DRAINED Pods Alive

Instead of auto-draining DRAINED pods, the operator leaves them running and provisions an extra replica to compensate. The DRAINED pod remains for human investigation.

**Code change:** In `handleScaleDown()`, remove the block that initiates drain for `role == "DRAINED"` pods. Instead, count DRAINED pods and increase the effective replica count:

```
effectiveReplicas = replicasPerCell + countDrainedPods
```

So `createMissingResources` iterates `0..effectiveReplicas-1`, creating a stand-in pod at the next available index.

### 2. Always Delete DRAINED Pod PVCs

When a DRAINED pod eventually goes through the drain state machine (e.g., admin does `kubectl delete pod`), always delete its PVC regardless of `WhenScaled` policy. DRAINED means data is known-bad — there's no value in retaining it.

**Code change:** In `cleanupDrainedPod()`, check for DRAINED role _before_ checking `WhenScaled`:

```go
// Always delete PVC for DRAINED pods — data is known-bad
if resolvePodRole(shard, pod.Name) == "DRAINED" {
    // delete PVC regardless of WhenScaled
}

// For non-DRAINED pods, respect WhenScaled policy
if policy.WhenScaled == DeletePVCRetentionPolicy {
    // existing logic
}
```

### 3. Recovery Workflows

Two paths for resolving a DRAINED pod:

**Remedy:** Someone investigates and fixes the data. Multiorch retries replication, succeeds, and converts the pooler back to `REPLICA`. The operator then sees `replicasPerCell + 0` DRAINED pods → the stand-in pod (highest index) becomes extra → normal scale-down logic drains it.

**Discard:** Admin does `kubectl delete pod` on the DRAINED pod. The operator catches it via `handleExternalDeletion`, runs drain state machine, deletes the PVC (DRAINED override), and creates a fresh replacement at the same index. The stand-in becomes extra and is cleaned up by scale-down.

Example walkthrough (replicas=3):

```
Initial:    pod-0 ✓, pod-1 (DRAINED), pod-2 ✓
Step 1:     Operator creates pod-3 as stand-in
            pod-0 ✓, pod-1 (DRAINED), pod-2 ✓, pod-3 ✓
Step 2:     Admin does: kubectl delete pod pod-1
            Operator drains pod-1, deletes its PVC
            pod-0 ✓, pod-2 ✓, pod-3 ✓ (index 1 missing)
Step 3:     Operator creates new pod-1 with fresh PVC (restores from backup)
            pod-0 ✓, pod-1 (restoring), pod-2 ✓, pod-3 ✓
Step 4:     pod-1 becomes healthy → pod-3 is extra → scale-down drains pod-3
Final:      pod-0 ✓, pod-1 ✓, pod-2 ✓
```

The health gate ensures pod-3 isn't removed until pod-1 is healthy.

### 4. Observability

DRAINED pods must be visible to administrators:

- **Pod label:** Operator sets `multigres.com/role=DRAINED` on the pod when detected. Enables `kubectl get pods -l multigres.com/role=DRAINED`.
- **Events:** Emit a warning event on the Shard when a pod is detected as DRAINED and when a stand-in is provisioned.
- **Observer:** Surface DRAINED pods in the observer's `/api/status` diagnostics endpoint.

### 5. Change `WhenScaled` Default to `Delete`

The default for `WhenScaled` changes from `Retain` to `Delete`. With `Retain`, scaled-down PVCs persist and are reused on scale-up — fast but risky if data had issues. With `Delete`, scale-up always restores from pgbackrest backup — slower but safer. pgbackrest should be the source of truth for data recovery.

Users with large databases (500GB+) who want fast scale-up can explicitly set `WhenScaled: Retain`, accepting the tradeoff.

## Open Questions

1. **Will `DRAINED` always mean diverged/unrecoverable data?** The protobuf definition is broader ("temporarily removed from serving traffic"). If DRAINED can mean "healthy but taken offline for rebalancing," the PVC handling needs to differ.

2. **What is the remedy workflow?** Does multiorch automatically retry `pg_rewind` periodically, or does the admin trigger something explicitly to convert a DRAINED pooler back to REPLICA?

3. **Should we remove `WhenScaled` entirely?** With the default changing to `Delete` and DRAINED pods always having their PVCs deleted, is there still enough value in offering `Retain` as an option? Removing it would simplify the code and eliminate a footgun. The tradeoff is slower scale-up for large databases.

## Implementation Summary

| Change | Files affected | Effort |
|---|---|---|
| Stop auto-draining DRAINED pods | `reconcile_pool_pods.go` (`handleScaleDown`) | Small |
| Provision extra replicas for DRAINED count | `reconcile_pool_pods.go` (`createMissingResources`) | Medium |
| Always delete DRAINED PVCs | `reconcile_pool_pods.go` (`cleanupDrainedPod`) | Small |
| Pod role label | `reconcile_pool_pods.go` (new label sync) | Small |
| Events for DRAINED detection | `reconcile_pool_pods.go` | Small |
| Observer integration | `observer/` | Small |
| Unit tests | `shard_controller_internal_test.go` | Medium |

Cannot fully integration-test until multiorch produces the DRAINED state in a test environment.
