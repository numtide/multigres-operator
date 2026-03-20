# Config & Fixture-Specific Scenarios

Additional mutation scenarios beyond the core set. Includes config changes, restart tests, and fixture-specific mutations.

---

### add-pod-annotations
**Tier:** quick | **Fast-path:** yes
**Tests:** Annotation propagation to stateless component pods without restart
**Success criteria:** Pod annotations contain the new key

> **Important:** `podAnnotations` is a field on `StatelessSpec`, used by **multiorch**, **multigateway**, and **multiadmin** only. It does NOT exist on `PoolSpec` — pool pods do not support `podAnnotations` via the CRD.

**How to patch:**
1. Merge patch to add `podAnnotations` to the first shard's multiorch:
   ```bash
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{
     "spec": {"databases": [{"name": "<db>", "default": true, "tablegroups": [{"name": "<tg>", "default": true, "shards": [{"name": "<shard>", "spec": {"multiorch": {"podAnnotations": {"chaos.multigres.com/test": "exerciser"}}}}]}]}]}
   }'
   ```
   Alternatively, patch multigateway annotations on a cell:
   ```bash
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{
     "spec": {"cells": [{"name": "<cell>", "spec": {"multigateway": {"podAnnotations": {"chaos.multigres.com/test": "exerciser"}}}}]}
   }'
   ```

**What to observe:**
- MultiOrch (or MultiGateway) pods should receive the annotation
- Annotation-only changes should NOT trigger pod restarts
- If pods restart, the operator may be over-reconciling — note this

**Teardown:** Remove the annotation via merge patch (set `podAnnotations` to `{}`).

### change-images
**Tier:** standard
**Tests:** Image update triggers cluster-wide rolling restart

**How to patch:**
1. Read current `.spec.images.postgres` (or check `api/v1alpha1/image_defaults.go` for the default).
2. Merge patch to set a different valid tag (e.g., `ghcr.io/multigres/pgctld:latest`).

**What to observe:**
- All pool pods restart with the new image (rolling update)
- Rolling update should preserve availability — at least one replica stays up at all times
- Replication re-establishes after all pods restart
- Watch for version compatibility issues between components

**Teardown:** Patch back to original image.

### add-tolerations
**Tier:** standard | **Fast-path:** no
**Tests:** Adding tolerations to pool spec propagates to pods via rolling update
**Applicable fixtures:** `minimal-retain`, `minimal-delete`

**How to patch:**
1. Read the current pool path for the first shard.
2. Merge patch adding a toleration:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"databases":[{"name":"postgres","tablegroups":[{"name":"default","shards":[{"name":"<shard>","spec":{"pools":{"<pool>":{"tolerations":[{"key":"test-key","operator":"Equal","value":"test-value","effect":"NoSchedule"}]}}}}]}]}]}}'
   ```

**What to observe:**
- Rolling update triggers on all pool pods
- After rollout, all pods have the toleration in `.spec.tolerations`
- Verify: `kubectl get pods -n <ns> -l multigres.com/pool=<pool> -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.tolerations}{"\n"}{end}'`
- No pods stuck in Pending (toleration key doesn't match any real taint)

**Teardown:** Patch tolerations back to `[]`.

### add-affinity
**Tier:** standard | **Fast-path:** no
**Tests:** Adding user affinity propagates to pods via rolling update
**Applicable fixtures:** `minimal-retain`, `minimal-delete`

**How to patch:**
1. Merge patch adding podAntiAffinity (uses `preferredDuring` so pods still schedule on single-node kind):
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"databases":[{"name":"postgres","tablegroups":[{"name":"default","shards":[{"name":"<shard>","spec":{"pools":{"<pool>":{"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":50,"podAffinityTerm":{"labelSelector":{"matchLabels":{"multigres.com/pool":"<pool>"}},"topologyKey":"kubernetes.io/hostname"}}]}}}}}}]}]}]}}'
   ```

**What to observe:**
- Rolling update triggers on all pool pods
- After rollout, all pods have affinity in `.spec.affinity`
- Verify: `kubectl get pods -n <ns> -l multigres.com/pool=<pool> -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.affinity}{"\n"}{end}'`
- No pods Pending (using `preferred` not `required`)

**Teardown:** Patch affinity to null.

---

### delete-operator-pod
**Tier:** quick | **Fast-path:** yes
**Tests:** Operator recovery, data plane independence
**Success criteria:** Operator pod Running+Ready, data plane pod count unchanged

**How to execute:**
1. Find operator pod: `kubectl get pods -n multigres-operator -l control-plane=controller-manager`
2. Delete it: `kubectl delete pod <name> -n multigres-operator`
3. Wait for new pod: `kubectl wait --for=condition=Ready pod -l control-plane=controller-manager -n multigres-operator --timeout=60s`

**What to observe:**
- **Data plane must be completely unaffected** — no pool pod restarts, no replication breaks
- Brief `operator-logs` errors expected as the old pod terminates
- New operator pod should resume reconciliation
- If data plane is affected by operator restart, that's a significant bug

**Teardown:** Not needed.

---

### change-pvc-deletion-policy
**Tier:** quick
**Tests:** PVC retention behavior changes propagate to child Shards

**How to patch:**
1. Read current `.spec.pvcDeletionPolicy` (whenDeleted, whenScaled).
2. Invert: Retain → Delete, or Delete → Retain.
3. Merge patch.

**What to observe:**
- Spec-only change, no pod restarts expected
- Verify policy propagated to child Shard CRs: `kubectl get shards -n <ns> -o yaml | grep -A2 pvcDeletionPolicy`

**Teardown:** Patch back to original.

### expand-pvc-storage
**Tier:** standard | **Fast-path:** yes
**Tests:** PVC expansion from pool storage.size patch, with observer verification
**Applicable fixtures:** `minimal-delete`, `minimal-retain`

**How to patch:**
1. Find the first pool's `storage.size` (typically `5Gi`).
2. Check StorageClass supports expansion: `kubectl get storageclass standard -o jsonpath='{.allowVolumeExpansion}'`
3. JSON patch to a larger value:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"databases":[{"name":"postgres","tablegroups":[{"name":"default","shards":[{"name":"<shard>","spec":{"pools":{"<pool>":{"storage":{"size":"10Gi"}}}}}]}]}]}}'
   ```

**What to observe:**
- Webhook accepts the expansion (not a shrink)
- PVC `.spec.resources.requests.storage` updates to the new size
- Operator does not restart pods for a storage-only change
- In kind, the `standard` StorageClass may not support expansion — an error event is infrastructure, not an operator bug

**Teardown:** Not reversible (Kubernetes blocks PVC shrinking).

### add-cell
**Tier:** lifecycle
**Tests:** Cell addition, new gateway deployment, topology registration

**How to patch:**
1. JSON patch to append to `.spec.cells/-`:
   ```json
   {"name": "us-east-2", "spec": {"multigateway": {"replicas": 1}}}
   ```
   Do NOT set `region` or `zone` unless the kind nodes have matching topology labels.

**What to observe:**
- New Cell CR created
- New MultiGateway Deployment appears for the cell
- Observer connectivity checks cover the new cell's gateway
- Topology updated in etcd to include new cell

**Teardown:** Not reversible (cells are append-only by design).

### add-pool
**Tier:** lifecycle
**Tests:** Pool addition to existing shard, new pod creation

**How to patch:**
1. Read the first cell name from `.spec.cells[0].name`.
2. Merge patch to add a new pool to the first shard:
   ```json
   {"new-pool": {"type": "readWrite", "cells": ["<cell-name>"], "replicasPerCell": 1, "storage": {"size": "2Gi"}}}
   ```

**What to observe:**
- New pool pods appear
- Replication set up for the new pool (replicas connect to primary)
- Observer connectivity checks include new pool endpoints

**Teardown:** Not reversible (pools are append-only by design).

### scale-etcd
**Tier:** standard
**Applicable:** Fixtures with inline `globalTopoServer.etcd` config

**How to patch:**
1. Read `.spec.globalTopoServer.etcd.replicas`. If absent, this fixture uses a template for etcd — skip.
2. Merge patch to increment replicas by 1.

**What to observe:**
- New etcd pod joins the StatefulSet
- Observer topology checks remain healthy
- etcd cluster membership updated correctly

**Teardown:** Patch back to original count.
