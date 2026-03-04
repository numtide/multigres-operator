# Storage Management

## PVC Deletion Policy

The operator supports fine-grained control over **Persistent Volume Claim (PVC) lifecycle management** for stateful components (TopoServers and Shard Pools). This allows you to decide whether PVCs should be automatically deleted or retained when resources are deleted or scaled down.

### Policy Options

The `pvcDeletionPolicy` field has two settings:

- **`whenDeleted`**: Controls what happens to PVCs when the entire MultigresCluster (or a component like a TopoServer) is deleted.
  - `Retain` (default): PVCs are preserved for manual review and potential data recovery
  - `Delete`: PVCs are automatically deleted along with the cluster

- **`whenScaled`**: Controls what happens to PVCs when reducing the number of replicas (e.g., scaling from 3 pods down to 1 pod).
  - `Retain` (default): PVCs from scaled-down pods are kept for potential scale-up
  - `Delete`: PVCs are automatically deleted when pods are removed

### Safe Defaults

**By default, the operator uses `Retain/Retain`** for maximum data safety. This means:
- Deleting a cluster will **not** delete your data volumes
- Scaling down will **not** delete the PVCs from removed pods

This is a deliberate choice to prevent accidental data loss.

### Where to Set the Policy

The `pvcDeletionPolicy` can be set at multiple levels in the hierarchy, with more specific settings overriding general ones:

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: my-cluster
spec:
  # Cluster-level policy (applies to all components unless overridden)
  pvcDeletionPolicy:
    whenDeleted: Retain  # Safe: keep data when cluster is deleted
    whenScaled: Delete   # Aggressive: auto-cleanup when scaling down

  globalTopoServer:
    # Override for GlobalTopoServer specifically
    pvcDeletionPolicy:
      whenDeleted: Delete  # Different policy for topo server
      whenScaled: Retain

  databases:
    - name: postgres
      tableGroups:
        - name: default
          # Override for this specific TableGroup
          pvcDeletionPolicy:
            whenDeleted: Retain
            whenScaled: Retain
          shards:
            - name: "0-inf"
              # Override for this specific shard
              spec:
                pvcDeletionPolicy:
                  whenDeleted: Delete
```

The policy is merged hierarchically:
1. **Shard-level** policy (most specific)
2. **TableGroup-level** policy
3. **Cluster-level** policy
4. **Template defaults** (CoreTemplate, ShardTemplate)
5. **Operator defaults** (Retain/Retain)

**Note**: If a child policy specifies only `whenDeleted`, it will inherit `whenScaled` from its parent, and vice versa.

### Templates and PVC Policy

You can define PVC policies in templates for reuse:

```yaml
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: production-shard
spec:
  pvcDeletionPolicy:
    whenDeleted: Retain
    whenScaled: Retain
  # ... other shard config
---
apiVersion: multigres.com/v1alpha1
kind: CoreTemplate
metadata:
  name: ephemeral-topo
spec:
  globalTopoServer:
    pvcDeletionPolicy:
      whenDeleted: Delete
      whenScaled: Delete
```

### Important Caveats

⚠️ **Data Loss Risk**: Setting `whenDeleted: Delete` means **permanent data loss** when the cluster is deleted. Use this only for:
- Development/testing environments
- Ephemeral clusters
- Scenarios where data is backed up externally

⚠️ **Replica Scale-Down Behavior**: Setting `whenScaled: Delete` will **immediately delete PVCs** when the operator removes pods during scale-down. If you scale the replica count back up, new pods will start with **empty volumes** and will need to restore from backup. This is useful for:
- Reducing storage costs in non-production environments
- Stateless-like workloads where data is ephemeral

**Note**: This does NOT affect storage size. Changing PVC storage capacity is handled separately by the **PVC Volume Expansion** feature (see below).

✅ **Production Recommendation**: For production clusters, use the default `Retain/Retain` policy and implement proper backup/restore procedures.

---

## PVC Volume Expansion

The operator supports **in-place PVC volume expansion**. When you increase `storage.size` on a pool (or backup filesystem storage), the operator patches the existing PVC spec and Kubernetes handles the underlying volume expansion.

```yaml
spec:
  databases:
    - name: postgres
      tableGroups:
        - name: default
          shards:
            - name: "0-inf"
              spec:
                pools:
                  main-app:
                    storage:
                      size: "200Gi"  # ← Increase from 100Gi to 200Gi
```

**Requirements:**
- The `StorageClass` must have `allowVolumeExpansion: true`
- Volume expansion is **grow-only** — decreasing `storage.size` is rejected at admission

**Behavior:**
- Most modern CSI drivers (EBS CSI ≥ v1.5, GCE PD CSI) expand the filesystem **online without pod restart**
- For drivers that require restart, the operator detects the `FileSystemResizePending` PVC condition and drains the affected pod automatically

> [!IMPORTANT]
> If your `StorageClass` does not have `allowVolumeExpansion: true`, the Kubernetes API will reject the PVC update and the operator will emit a warning event. Check your StorageClass before changing storage sizes.
