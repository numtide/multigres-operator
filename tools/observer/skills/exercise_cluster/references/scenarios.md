# Scenario Catalog

Each scenario describes a mutation to apply to a live MultigresCluster CR. For every scenario:

1. **Read the live CR first** — `kubectl get multigrescluster <name> -n <ns> -o yaml`
2. Understand the CR structure before patching (spec vs overrides, pool names, cell names)
3. Construct the correct patch based on what you see, not hardcoded paths
4. Apply via `kubectl patch`, then run the Stability Verification Protocol
5. Teardown if listed, then re-verify stability

> **Namespace:** All kubectl commands must use `KUBECONFIG=$(pwd)/kubeconfig.yaml`.
>
> **Reusable patches:** The `patches/` directory contains parameterized shell scripts for common mutations. Set environment variables and run the script instead of constructing kubectl patches inline. See each script's header for required variables.

---

## Scale Scenarios

### scale-up-pool-replicas
**Tier:** standard | **Fast-path:** yes
**Tests:** Pool replica increase, new pod creation, replication setup
**Success criteria:** Pool pod count per cell == new replicasPerCell, all pods Running+Ready

**How to patch:**
1. Find all pools in the CR. They may be at:
   - `.spec.databases[].tablegroups[].shards[].spec.pools.<name>` (inline spec)
   - `.spec.databases[].tablegroups[].shards[].overrides.pools.<name>` (override-based)
2. Pick the first readWrite pool. Note its name and current `replicasPerCell`.
3. JSON patch to increment replicasPerCell by 1.

**What to observe:**
- New pod appears with correct labels and joins the StatefulSet-like set
- Observer connectivity checks include the new pod after startup
- Observer replication check shows the new pod as a connected replica
- No split-brain findings at any point

**Teardown:** Patch replicasPerCell back to original value.

### scale-down-pool-replicas
**Tier:** standard | **Fast-path:** no (drain machine)
**Tests:** Drain state machine, graceful pod removal, PVC handling
**Negative assertion:** Resource Removal — pod count decreased by 1 after drain completes

**How to patch:**
1. Same discovery as scale-up. Current replicasPerCell must be > 3.
2. JSON patch to decrement by 1.

> **IMPORTANT:** Never scale below `replicasPerCell: 3`. Multigres uses `ANY_2` synchronous quorum by default, which requires at least 3 poolers to function correctly. Scaling to 2 or fewer causes write stalls and spurious recovery actions that are unrelated to operator logic and will waste investigation time.

**What to observe:**
- Drain state annotations should progress: `DrainStateRequested` → `DrainStateDraining` → `DrainStateAcknowledged` → `DrainStateReadyForDeletion`
- Observer `drain-state` findings during the drain are EXPECTED and normal
- Drain should complete within the timeouts (30s / 5min / 30s per state)
- After drain completes, the pod should terminate
- **If drain gets stuck** (same state beyond its timeout) — that is a bug
- After full stabilization, no drain-state errors should remain

**Teardown:** Patch replicasPerCell back to original value.

### scale-multigateway-replicas
**Tier:** standard | **Fast-path:** yes
**Tests:** Gateway Deployment scaling
**Success criteria:** Deployment `.status.readyReplicas` == target

**How to patch:**
1. Read `.spec.cells[0].name` to get the cell name.
2. Determine if the cell uses `.spec.multigateway` or `.overrides.multigateway`.
3. Read current `replicas` value.
4. Merge patch to increment by 1 (use the correct path: spec or overrides).

**What to observe:**
- New gateway pod appears in the cell's MultiGateway Deployment
- Observer connectivity checks cover the new gateway endpoint

**Teardown:** Patch replicas back to original.

---

## Config Scenarios

### update-resource-limits
**Tier:** standard | **Fast-path:** yes
**Tests:** Rolling update triggered by resource change, pod recreation order
**Success criteria:** All pool pods Running+Ready with updated resource values

**How to patch:**
1. Find the first pool path (spec or overrides, as above).
2. JSON patch to set `postgres.resources`:
   ```json
   {"requests": {"cpu": "250m", "memory": "384Mi"}, "limits": {"cpu": "500m", "memory": "512Mi"}}
   ```

**What to observe:**
- Pods restart one by one (rolling update via the resource-handler)
- **Brief connectivity errors during each pod's restart are expected** — these should resolve as each pod comes back up
- After all pods restart, full connectivity restored
- Replication fully re-established across all replicas
- The key bug signal: errors that persist AFTER all pods are Ready and past grace period

**Teardown:** Remove the resources field or restore original values.

### add-pod-annotations
**Tier:** quick | **Fast-path:** yes
**Tests:** Annotation propagation to child resources without restart
**Success criteria:** Pod annotations contain the new key

**How to patch:**
1. Merge patch to add `podAnnotations` to the first shard's multiorch:
   ```json
   {"chaos.multigres.com/test": "exerciser"}
   ```

**What to observe:**
- MultiOrch pods should receive the annotation (verify with `kubectl get pods -o yaml`)
- Annotation-only changes should NOT trigger pod restarts
- If pods restart, that may indicate the operator is over-reconciling — note this

**Teardown:** Remove the annotation via JSON patch.

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

---

## Restart Scenarios

### delete-pool-pod
**Tier:** standard | **Fast-path:** yes
**Tests:** Pod recreation, replication recovery, potential failover
**Success criteria:** Replacement pod Running+Ready, total pod count unchanged

**How to execute:**
1. List pool pods: `kubectl get pods -n <ns> -l app.kubernetes.io/component=shard-pool`
2. Check which pod is primary (from observer's replication probe data or pod labels).
3. Delete the pod: `kubectl delete pod <name> -n <ns>`

**What to observe:**
- Pod is recreated by the operator
- **If primary was deleted**: multiorch should perform failover. Brief write unavailability is expected. A new primary should be elected. Observer should detect the failover sequence.
- **If replica was deleted**: primary maintains writes, replica should rejoin replication after restart.
- After stabilization: replication fully healthy, no split-brain, connectivity restored

**Teardown:** Not needed — Kubernetes recreates the pod.

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

## Lifecycle Scenarios

### delete-and-recreate-cluster
**Tier:** lifecycle | **Fast-path:** no
**Tests:** Full teardown, cascade deletion via ownerReferences, fresh bootstrap
**Negative assertion:** Cleanup Verification — all managed pods and child CRs gone before recreate

**How to execute:**
1. Save current CR: `kubectl get multigrescluster <name> -n <ns> -o yaml > /tmp/cluster-backup.yaml`
2. Delete: `kubectl delete multigrescluster <name> -n <ns>`
3. Wait for all managed pods to terminate: `kubectl get pods -n <ns> -l app.kubernetes.io/part-of=multigres`
4. Recreate: `kubectl apply -f /tmp/cluster-backup.yaml`

**What to observe:**
- During deletion: all child resources (Cell, Shard, TopoServer, pods, services) should cascade-delete via ownerReferences. The operator uses NO finalizers.
- If any child resources linger after the parent is gone, that's an ownerReference bug.
- During recreation: full bootstrap sequence — TopoServer, Cells, Shards, pool pods
- After recreation: full stability verification with lifecycle tier (10 min timeout, 90s observation)

**Teardown:** Not needed — cluster is back.

---

## Fixture-Specific Scenarios

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
**Tier:** standard
**Tests:** PVC expansion request handling

**How to patch:**
1. Find the first pool's `storage.size`.
2. JSON patch to a larger value (e.g., "100Gi").

**What to observe:**
- PVC resize requests are created
- In kind, the `standard` StorageClass may not support expansion — an error event is infrastructure, not an operator bug. Check StorageClass `allowVolumeExpansion` first.

**Teardown:** Not reversible (Kubernetes blocks PVC shrinking).

### add-cell
**Tier:** lifecycle
**Tests:** Cell addition, new gateway deployment, topology registration

**How to patch:**
1. JSON patch to append to `.spec.cells/-`:
   ```json
   {"name": "us-east-2", "spec": {"multigateway": {"replicas": 1}}}
   ```
   Do NOT set `region` or `zone` unless the kind nodes have matching topology labels. In kind, cells without region/zone schedule anywhere.

**What to observe:**
- New Cell CR created
- New MultiGateway Deployment appears for the cell
- Observer connectivity checks cover the new cell's gateway
- Topology updated in etcd to include new cell

**Teardown:** Not reversible (cells are append-only by design).

### remove-cell (Negative Test)
**Tests:** Webhook enforces append-only cell rule
**Negative assertion:** Webhook Rejection — exit code != 0, stderr contains `cells are append-only` or `cannot remove cell`

Attempt to remove `.spec.cells/0` via JSON patch:
```bash
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl patch multigrescluster <name> -n <ns> \
  --type=json -p '[{"op":"remove","path":"/spec/cells/0"}]' 2>&1
echo "exit_code=$?"
```
**Expected:** Webhook rejects with validation error.
**If it succeeds (exit code 0):** Critical bug — webhook not enforcing append-only invariant.

### add-pool
**Tier:** lifecycle
**Tests:** Pool addition to existing shard, new pod creation

**How to patch:**
1. Read the first cell name from `.spec.cells[0].name`.
2. Merge patch to add a new pool to the first shard:
   ```json
   {"new-ro-pool": {"type": "readOnly", "cells": ["<cell-name>"], "replicasPerCell": 1, "storage": {"size": "2Gi"}}}
   ```

**What to observe:**
- New pool pods appear
- Replication set up for the new pool (replicas connect to primary)
- Observer connectivity checks include new pool endpoints

**Teardown:** Not reversible (pools are append-only by design).

### remove-pool (Negative Test)
**Tests:** Webhook enforces append-only pool rule
**Negative assertion:** Webhook Rejection — exit code != 0, stderr contains `pools are append-only` or `cannot remove pool`

Attempt to remove the first pool via JSON patch:
```bash
# First read the CR to find the correct pool path
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl patch multigrescluster <name> -n <ns> \
  --type=json -p '[{"op":"remove","path":"/spec/databases/0/tablegroups/0/shards/0/spec/pools/<pool-name>"}]' 2>&1
echo "exit_code=$?"
```
**Expected:** Webhook rejects with validation error.
**If it succeeds (exit code 0):** Critical bug — webhook not enforcing append-only invariant.

### switch-template
**Tier:** lifecycle
**Applicable:** Template-based fixtures (`templated-full`)

**How to patch:**
1. Read current `.spec.templateDefaults.cellTemplate`.
2. Create a new CellTemplate CR with slightly different config (e.g., different gateway replicas).
3. Merge patch `templateDefaults.cellTemplate` to point to the new template.

**What to observe:**
- Template change triggers reconciliation of all cells using that template
- Cell specs update to match new template's values
- Gateway deployments scale to match new template

**Teardown:** Patch back to original template name.

### update-template
**Tier:** standard
**Applicable:** Template-based fixtures (`templated-full`)

**How to patch:**
1. Read the cellTemplate name from the cluster's `templateDefaults`.
2. Patch the CellTemplate CR directly (not the MultigresCluster):
   ```bash
   kubectl patch celltemplate <name> -n <ns> --type=merge -p '{"spec":{"multigateway":{"replicas":3}}}'
   ```

**What to observe:**
- Template content change propagates to all cells using it
- Cells reconcile with updated values

**Teardown:** Patch template back to original values.

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

---

## Template & Override Scenarios

These scenarios test the template resolution and override merging system. They are applicable to fixtures that use `templateDefaults` and/or inline overrides (`templated-full`, `overrides-complex`).

### verify-template-propagation
**Tier:** standard
**Applicable:** All template-based fixtures
**Tests:** Full Template Verification Protocol — systematic field-by-field comparison of deployed resources against template definitions

**How to execute:**
1. Read all template CRs referenced by the cluster (`kubectl get coretemplate`, `celltemplate`, `shardtemplate`).
2. Read the cluster CR to identify overrides and inline specs.
3. Build the expected values map (template base → override → inline → defaults).
4. Query every deployed resource (etcd STS, multiadmin Deploy, multigateway Deploy, multiorch Deploy, pool pods, PVCs).
5. Compare field-by-field and classify each as PASS, COINCIDENCE, or FAIL.

**What to observe:**
- Every template-defined field should appear in the deployed resource with the correct value
- Overrides should take precedence over template values
- Hardcoded defaults should only appear for fields not set by template or override
- COINCIDENCE results indicate the fixture should use non-default values

**Teardown:** Not needed (read-only verification).

### template-partial-override
**Tier:** standard
**Applicable:** `overrides-complex` or any fixture with both templates and overrides
**Tests:** Partial override merging — override changes one field, template provides the rest

**How to execute:**
1. Read the ShardTemplate to note its multiorch resources and replicas.
2. Patch the cluster's shard override to add ONLY a resource override:
   ```bash
   kubectl patch multigrescluster <name> -n <ns> --type=json -p '[
     {"op":"add","path":"/spec/databases/0/tablegroups/0/shards/0/overrides/multiorch/resources",
      "value":{"requests":{"cpu":"100m","memory":"128Mi"}}}
   ]'
   ```
3. Verify multiorch gets: replicas from ShardTemplate (unchanged), resources from the override (new value).
4. This confirms partial overrides don't clobber unset template fields.

**What to observe:**
- Multiorch replicas remain at the template/previous-override value (not reset to default)
- Multiorch resources match the new override
- No pod restarts unless the resource change triggers a rolling update

**Teardown:** Remove the resource override via JSON patch.

### update-core-template-cr
**Tier:** standard
**Applicable:** Template-based fixtures with CoreTemplate
**Tests:** Mutating a CoreTemplate CR propagates changes to all derived resources

**How to execute:**
1. Read current CoreTemplate CR: `kubectl get coretemplate <name> -n <ns> -o yaml`
2. Patch multiadmin replicas:
   ```bash
   kubectl patch coretemplate <name> -n <ns> --type=merge -p '{"spec":{"multiadmin":{"replicas":3}}}'
   ```
3. Wait for reconciliation (watch multiadmin Deployment).
4. Verify multiadmin Deployment now has 3 replicas.
5. Verify etcd StatefulSet is unchanged (wasn't patched).

**What to observe:**
- Controller detects template change and re-reconciles
- Multiadmin Deployment scales to match new template value
- Other CoreTemplate-derived resources (etcd) remain untouched
- If multiadmin does NOT update, the controller is not watching template changes

**Teardown:** Patch CoreTemplate back to original replicas.

### update-cell-template-cr
**Tier:** standard
**Applicable:** Template-based fixtures with CellTemplate
**Tests:** Mutating a CellTemplate CR propagates to all gateway deployments

**How to execute:**
1. Read current CellTemplate CR.
2. Patch multigateway replicas:
   ```bash
   kubectl patch celltemplate <name> -n <ns> --type=merge -p '{"spec":{"multigateway":{"replicas":3}}}'
   ```
3. Wait for reconciliation.
4. Verify all gateway Deployments update to 3 replicas.

**What to observe:**
- All cells referencing this template update their gateway replica count
- Observer connectivity checks cover any new gateway pods

**Teardown:** Patch CellTemplate back to original replicas.

### update-shard-template-pool
**Tier:** standard
**Applicable:** Template-based fixtures with ShardTemplate
**Tests:** Mutating ShardTemplate pool config triggers pod/PVC changes

**How to execute:**
1. Read current ShardTemplate CR.
2. Patch pool resources:
   ```bash
   kubectl patch shardtemplate <name> -n <ns> --type=merge -p '{"spec":{"pools":{"read-write":{"postgres":{"resources":{"requests":{"cpu":"200m","memory":"256Mi"}}}}}}}'
   ```
3. Wait for reconciliation and rolling restart.

**What to observe:**
- Pool pods restart with new resource limits (rolling update)
- Brief connectivity errors during restart are expected
- After restart, all pods have the new resources
- Replication re-establishes fully

**Teardown:** Patch ShardTemplate back to original resources.

### override-wins-over-template
**Tier:** standard
**Applicable:** `overrides-complex` (requires both template and override for the same field)
**Tests:** Explicit override takes precedence when template is mutated to conflict

**How to execute:**
1. Confirm the cluster has an inline override for multiadmin replicas (e.g., replicas=2).
2. Patch the CoreTemplate to set a DIFFERENT multiadmin replicas value:
   ```bash
   kubectl patch coretemplate <name> -n <ns> --type=merge -p '{"spec":{"multiadmin":{"replicas":5}}}'
   ```
3. Wait for reconciliation.
4. Verify multiadmin Deployment stays at 2 (the override value), NOT 5 (the template value).

**What to observe:**
- Override precedence is preserved: inline spec > template
- If multiadmin changes to 5, the merge logic is broken (template incorrectly winning)
- If multiadmin stays at 2, the precedence chain is correct

**Teardown:** Patch CoreTemplate back to original value.

### per-shard-template-vs-global
**Tier:** lifecycle
**Applicable:** Fixtures that can add a second shard (append-only)
**Tests:** Per-shard ShardTemplate reference overrides global TemplateDefaults.ShardTemplate

**How to execute:**
1. Create a second ShardTemplate CR with different pool config:
   ```bash
   kubectl apply -f - <<EOF
   apiVersion: multigres.com/v1alpha1
   kind: ShardTemplate
   metadata:
     name: alt-shard-template
     namespace: <ns>
   spec:
     multiorch:
       replicas: 1
     pools:
       alt-rw:
         type: readWrite
         replicasPerCell: 4
         storage:
           size: "3Gi"
   EOF
   ```
2. Read the cluster CR to find the first cell name.
3. Add a new shard that references this per-shard template:
   ```bash
   kubectl patch multigrescluster <name> -n <ns> --type=json -p '[
     {"op":"add","path":"/spec/databases/0/tablegroups/0/shards/-",
      "value":{"name":"alt-shard","shardTemplate":"alt-shard-template",
               "overrides":{"pools":{"alt-rw":{"cells":["<cell-name>"]}}}}}
   ]'
   ```
4. Wait for the new shard to come up.
5. Verify original shard uses global ShardTemplate values.
6. Verify new shard uses `alt-shard-template` values (multiorch=1, pool=alt-rw, storage=3Gi).

**What to observe:**
- Two shards with different template sources coexist
- Per-shard template takes precedence over global TemplateDefaults.ShardTemplate
- If both shards end up with the same config, per-shard template override is broken

**Teardown:** Not reversible (shards are append-only).

### pvc-deletion-policy-inheritance
**Tier:** quick
**Applicable:** Template-based fixtures
**Tests:** PVCDeletionPolicy inheritance chain: cluster → ShardTemplate → shard override

**How to execute:**
1. Read current PVCDeletionPolicy at cluster level: `kubectl get multigrescluster <name> -o jsonpath='{.spec.pvcDeletionPolicy}'`
2. Read Shard CRs to see current resolved policy: `kubectl get shard -n <ns> -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.pvcDeletionPolicy}{"\n"}{end}'`
3. Patch ShardTemplate to set a different PVCDeletionPolicy:
   ```bash
   kubectl patch shardtemplate <name> -n <ns> --type=merge -p '{"spec":{"pvcDeletionPolicy":{"whenDeleted":"Retain","whenScaled":"Retain"}}}'
   ```
4. Wait for reconciliation. Verify Shard CRs pick up the template's policy.
5. Patch the cluster's shard override to set yet another policy:
   ```bash
   kubectl patch multigrescluster <name> -n <ns> --type=json -p '[
     {"op":"add","path":"/spec/databases/0/tablegroups/0/shards/0/overrides/pvcDeletionPolicy",
      "value":{"whenDeleted":"Delete","whenScaled":"Delete"}}
   ]'
   ```
6. Verify the override wins over the template.

**What to observe:**
- Spec-only changes, no pod restarts expected
- ShardTemplate PVCDeletionPolicy propagates to Shard CRs
- Shard-level override wins over ShardTemplate policy
- Cluster-level policy is the weakest in the chain

**Teardown:** Revert both patches.

### template-pod-labels-merge
**Tier:** quick
**Applicable:** Fixtures with podLabels at template and override levels
**Tests:** PodLabels from template and override are merged additively, not replaced

**How to execute:**
1. Verify the ShardTemplate has `podLabels` set (e.g., `{"source": "shard-template"}`).
2. Verify the cluster's shard override has different `podLabels` (e.g., `{"from": "override"}`).
3. Check multiorch pods for both labels:
   ```bash
   kubectl get pods -n <ns> -l app.kubernetes.io/component=multiorch -o jsonpath='{range .items[0]}{.metadata.labels}{"\n"}{end}'
   ```
4. Verify both `source=shard-template` AND `from=override` are present.

**What to observe:**
- Labels are merged (additive) — both template and override labels appear
- If only override labels appear, the template labels were clobbered (merge bug)
- If only template labels appear, the override wasn't applied
- Standard Kubernetes labels (`app.kubernetes.io/*`) should always be present regardless

**Teardown:** Not needed (read-only verification). If labels were added for this test, remove them.

---

## Concurrent Mutation Scenarios

These scenarios fire two mutations in rapid succession to test race conditions in the operator's reconciliation loop. Only run in **full** execution mode. Use the **Concurrent Mutation Protocol** from SKILL.md.

**Applicable fixtures:** `minimal-retain`, `minimal-delete`, `templated-full`.

### concurrent-scale-and-delete-pod
**Tier:** standard | **Fast-path:** no
**Tests:** Overlapping scale-up reconciliation with pod replacement

**How to execute:**
1. Record baseline: pool pod count, current replicasPerCell.
2. **Mutation A**: Scale pool replicas +1 (JSON patch replicasPerCell).
3. **Mutation B** (within 2-3 seconds): Delete a pool pod (`kubectl delete pod <name>`).
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Total pool pod count == original + 1 (scale succeeded)
- Deleted pod has been replaced (no missing pods)
- All pods Running+Ready
- Observer connectivity and replication healthy

**What to observe:**
- The operator must handle the scale-up and the pod deletion without interfering
- Watch for pods stuck in Pending or pods being double-created
- Replication should include all pods after stabilization

**Teardown:** Scale replicasPerCell back to original value.

### concurrent-config-and-scale
**Tier:** standard | **Fast-path:** no
**Tests:** Overlapping resource update (rolling restart) with gateway scaling

**How to execute:**
1. Record baseline: pool pod resources, gateway replicas.
2. **Mutation A**: Update pool postgres resources (JSON patch).
3. **Mutation B** (within 2-3 seconds): Scale multigateway replicas +1 (merge patch).
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Pool pods have updated resource values
- Gateway Deployment readyReplicas == new target
- All pods Running+Ready

**What to observe:**
- Rolling restart from resource change should not block gateway scale-up
- Both controllers (resource-handler and cell controller) reconcile without conflict
- No resource version conflicts causing retry storms

**Teardown:** Restore original resources and gateway replicas.

### concurrent-template-and-pod-delete
**Tier:** standard | **Fast-path:** no
**Applicable:** Template-based fixtures only
**Tests:** Template CR mutation during pod replacement

**How to execute:**
1. Record baseline: template values, pool pod count.
2. **Mutation A**: Patch the ShardTemplate CR (e.g., change multiorch resources).
3. **Mutation B** (within 2-3 seconds): Delete a pool pod.
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Template change propagated to multiorch pods
- Deleted pool pod replaced
- All pods Running+Ready

**What to observe:**
- Template reconciliation and pod replacement must not interfere
- Watch for template values not propagating because the controller was busy with pod replacement
- Check observer history for flapping findings

**Teardown:** Restore template to original values.

### concurrent-dual-scale
**Tier:** standard | **Fast-path:** no
**Tests:** Two independent scale operations at the same time

**How to execute:**
1. Record baseline: pool replicasPerCell, gateway replicas.
2. **Mutation A**: Scale pool replicas +1.
3. **Mutation B** (within 2-3 seconds): Scale multigateway replicas +1.
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Pool pod count per cell == new replicasPerCell
- Gateway Deployment readyReplicas == new target
- All pods Running+Ready

**What to observe:**
- Both scale operations should succeed independently
- Resource version conflicts should be retried cleanly by the operator
- No pods stuck in Pending or ContainerCreating

**Teardown:** Restore both replica counts to original values.

## Webhook Rejection Scenarios

These are **negative tests**: the mutation MUST be rejected by the admission webhook. The cluster should remain unchanged after each attempt. Always verify the cluster is still Healthy and the observer reports no new findings after the rejection.

### storage-shrink-rejection
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects storage size decreases (PVC shrink not supported)

**How to execute:**
1. Record current pool storage size from the MultigresCluster spec.
2. **Mutation**: Attempt to shrink storage size to a smaller value:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"<pool>","storage":{"size":"100Mi"}}]}]}}'
   ```
3. **Expected**: kubectl returns an error containing "storage shrink" or similar rejection.
4. Verify cluster spec is unchanged: `kubectl get multigrescluster <name> -n <ns> -o jsonpath='{.spec.shards[0].pools[0].storage.size}'`
5. Run targeted observer check: `curl http://localhost:9090/api/check?categories=crd-status`

**Success criteria:**
- Webhook rejects the request (HTTP 422 or admission denied)
- Cluster remains Healthy with original storage size
- Observer reports zero new findings

**Teardown:** None needed — mutation was rejected.

### etcd-replicas-immutable
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects etcd replica count changes after creation

**How to execute:**
1. Record current etcd replica count.
2. **Mutation**: Attempt to change etcd replicas:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"topoServer":{"replicas":5}}}'
   ```
3. **Expected**: kubectl returns an error containing "etcd replicas" or "immutable".
4. Verify TopoServer StatefulSet unchanged.

**Success criteria:**
- Webhook rejects the request
- TopoServer StatefulSet replicas unchanged
- Cluster remains Healthy

**Teardown:** None needed.

### invalid-template-reference
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects references to non-existent templates

**How to execute:**
1. **Mutation**: Attempt to set a non-existent template reference:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"coreTemplateRef":"nonexistent-template"}}'
   ```
2. **Expected**: kubectl returns an error containing "template" or "not found".
3. Verify cluster spec unchanged.

**Success criteria:**
- Webhook rejects the request
- Cluster spec retains original template reference
- Cluster remains Healthy

**Teardown:** None needed.

### invalid-pool-name
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects pool names with invalid characters

**How to execute:**
1. **Mutation**: Attempt to add a pool with an invalid name:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"INVALID_NAME!","replicasPerCell":1,"cells":["<cell>"]}]}]}}'
   ```
2. **Expected**: kubectl returns a validation error.
3. Verify cluster spec unchanged.

**Success criteria:**
- Webhook or CRD validation rejects the request
- Cluster remains Healthy

**Teardown:** None needed.

## Drain Scenarios

### drain-abort-on-scale
**Tier:** standard | **Fast-path:** no
**Tests:** Scaling back up while a drain is in progress

**How to execute:**
1. Record baseline replicasPerCell (must be >= 2 for scale-down).
2. **Mutation A**: Scale down pool replicas by 1 to trigger a drain.
3. **Watch** drain annotations on pods: `kubectl get pods -n <ns> -l multigres.com/shard=<shard> -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations.drain\.multigres\.com/state}{"\n"}{end}'`
4. **While drain is in `draining` state** (within 30s-60s of scale-down), **Mutation B**: Scale replicas back to original value.
5. Run full Stability Verification Protocol.

**Success criteria:**
- Drain either completes or aborts cleanly — no stuck drain annotations
- Final pod count matches the restored replicasPerCell
- All pods Running+Ready with correct roles
- Observer drain-state check shows no persistent findings
- No pods in `ready-for-deletion` state without corresponding deletion

**What to observe:**
- The operator should handle the conflicting intent gracefully
- Watch for pods stuck in draining state indefinitely
- Check for race conditions: drain deleting a pod while scale-up is creating one

**Teardown:** If replicas don't match original, restore manually.

## Backup Scenarios

### verify-backup-status-fields
**Tier:** standard | **Fast-path:** yes
**Tests:** Backup status fields are populated correctly
**Fixtures:** s3-backup (requires backup infrastructure)

**How to execute:**
1. Deploy the `s3-backup` fixture (skip if S3 not available).
2. Wait for cluster to reach Healthy phase.
3. **Read** backup-related status:
   ```
   kubectl get shard -n <ns> -o jsonpath='{range .items[*]}{.metadata.name}: lastBackupTime={.status.lastBackupTime} lastBackupType={.status.lastBackupType}{"\n"}{end}'
   ```
4. Check BackupHealthy condition:
   ```
   kubectl get shard -n <ns> -o jsonpath='{range .items[*]}{.metadata.name}: {range .status.conditions[?(@.type=="BackupHealthy")]}{.status} {.reason}{end}{"\n"}{end}'
   ```
5. Run observer backup staleness check: `curl http://localhost:9090/api/check?categories=crd-status`

**Success criteria:**
- `lastBackupTime` is populated (not nil) after initial backup completes
- `lastBackupType` is one of: `full`, `diff`, `incr`
- `BackupHealthy` condition is `True`
- Observer reports no backup-related findings

**Teardown:** Standard fixture teardown.

## Template Scenarios (Extended)

### template-affinity-propagation
**Tier:** standard | **Fast-path:** no
**Tests:** Affinity settings in templates propagate to all managed pods
**Fixtures:** templated-full

**How to execute:**
1. Deploy the `templated-full` fixture which includes affinity in its template.
2. Wait for Healthy phase.
3. **Verify pool pods**:
   ```
   kubectl get pods -n <ns> -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.affinity}{"\n"}{end}'
   ```
4. **Verify multiorch pods**:
   ```
   kubectl get pods -n <ns> -l app.kubernetes.io/component=multiorch -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.affinity}{"\n"}{end}'
   ```
5. **Verify multigateway pods**:
   ```
   kubectl get pods -n <ns> -l app.kubernetes.io/component=multigateway -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.affinity}{"\n"}{end}'
   ```
6. Compare each pod's affinity spec against the template's affinity definition.

**Success criteria:**
- All pool, multiorch, and multigateway pods have the affinity from the template
- Override precedence is respected (shard override > template)
- Run Template Verification Protocol (TVP) for full validation

**Teardown:** Standard fixture teardown.

## Rolling Update Scenarios

### rolling-update-drain-path
**Tier:** standard | **Fast-path:** no
**Tests:** Config changes trigger proper drain→replace cycle through the drain state machine

**How to execute:**
1. Record baseline pod names and their creation timestamps.
2. **Mutation**: Change pool resource limits to trigger a rolling update:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"<pool>","resources":{"limits":{"memory":"512Mi"}}}]}]}}'
   ```
3. **Monitor** drain state transitions on pods during the rolling update:
   ```
   watch -n2 'kubectl get pods -n <ns> -l multigres.com/shard=<shard> -o custom-columns=NAME:.metadata.name,DRAIN:.metadata.annotations.drain\.multigres\.com/state,READY:.status.conditions[?\(@.type==\"Ready\"\)].status,AGE:.metadata.creationTimestamp'
   ```
4. Verify pods are replaced one at a time (no concurrent drains on same shard).
5. Run full Stability Verification Protocol after all pods are replaced.

**Success criteria:**
- Each old pod goes through: requested → draining → acknowledged → ready-for-deletion → deleted
- New pod is created after old pod is deleted (serial replacement)
- No concurrent drains on the same shard at any point
- All new pods have the updated resource limits
- Observer drain-state check shows proper transitions with no timeouts

**What to observe:**
- Drain state machine must progress forward-only
- Watch for stuck drains (same state beyond timeout)
- Primary pod should be drained last (if applicable)
- Replication should remain healthy throughout (check observer replication findings)

**Teardown:** None needed — new state is valid.

## Failure Injection Scenarios

### etcd-unavailability
**Tier:** standard | **Fast-path:** no
**Tests:** Behavior when the topology store (etcd) becomes unavailable

**How to execute:**
1. Record baseline TopoServer StatefulSet state.
2. **Mutation**: Scale TopoServer to 0 replicas:
   ```
   kubectl scale statefulset <toposerver-name> -n <ns> --replicas=0
   ```
3. Wait 60s for the system to detect the failure.
4. **Observe**:
   - Observer topology check should report errors: `curl http://localhost:9090/api/check?categories=topology`
   - Shard/cluster phase should transition to Degraded
   - Data plane (pool pods) should continue serving reads
5. **Recovery**: Restore TopoServer replicas:
   ```
   kubectl scale statefulset <toposerver-name> -n <ns> --replicas=<original>
   ```
6. Run full Stability Verification Protocol.

**Success criteria:**
- Observer detects topology unavailability within 1-2 cycles
- Data plane continues serving read queries during etcd outage
- After recovery, topology is re-established
- Cluster returns to Healthy phase
- Observer history shows the topology errors as transient (not persistent)

**What to observe:**
- How long until the operator detects etcd is down
- Whether multiorch continues functioning with cached state
- Whether any pool pods restart unnecessarily
- Recovery time from etcd restore to Healthy phase

**Teardown:** Restore original StatefulSet replicas if not done in step 5.

### image-pull-backoff
**Tier:** standard | **Fast-path:** no
**Tests:** Behavior when a pool pod has an invalid image (simulates registry failure)

**How to execute:**
1. Record current pool container image.
2. **Mutation**: Patch one pool's image to a non-existent image:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"<pool>","image":"invalid-image:nonexistent"}]}]}}'
   ```
3. Wait 60-90s for the rolling update to start and the new pod to fail.
4. **Observe**:
   - New pod should enter `ImagePullBackOff` or `ErrImagePull` state
   - Observer pod-health check should detect the failing pod: `curl http://localhost:9090/api/check?categories=pod-health`
   - Shard phase should transition to Degraded
5. **Recovery**: Restore the valid image:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"<pool>","image":"<original-image>"}]}]}}'
   ```
6. Run full Stability Verification Protocol.

**Success criteria:**
- Observer detects ImagePullBackOff via pod-health check
- Shard phase transitions to Degraded
- After image restoration, pod recovers and shard returns to Healthy
- Observer history shows the pod-health findings as transient
- No data loss or replication breaks during the incident

**What to observe:**
- Whether the operator's rolling update blocks (doesn't replace more pods while one is failing)
- Whether the drain state machine is involved or bypassed
- Recovery time from image restore to Healthy

**Teardown:** Restore original image if not done in step 5.
