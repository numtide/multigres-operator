# Backup Architecture (Developer Internals)

For user-facing backup documentation, see [docs/backup-restore.md](../backup-restore.md).

## Overview

Backup configuration is defined at the `MultigresCluster` level and propagates down to Shards via a text-merge strategy. The operator supports two backends: **S3** (object storage) and **Filesystem** (PVC-based).

## The "Shared PVC" Design (Filesystem)

**The Old Design (Rejected):**
In early prototypes, we considered a "One PVC per Pool per Cell" model.
- **Issue:** pgBackRest requires that all members of a stanza (Shard) share the **exact same physical repository**.
- **Explanation:** If Replica A (writing to Vol A) takes a backup, and Replica B (writing to Vol B) tries to restore, Replica B will look in its own Vol B at the configured path (`/backups/repo...`) and find it empty or different.
- **Result:** You create N isolated, split-brain repositories. Restores, archiving, and replication become impossible because the replicas cannot see each other's data.

**The New Design (Current):**
We implemented a **One Shared PVC per Shard per Cell** model.
- **Mechanism:** All replicas of a Shard (within the same Cell) mount a single, shared PVC at `/backups`.
- **Naming:** `backup-data-{cluster}-{db}-{tg}-{shard}-{cell}`.
- **Benefit:**
    1.  **Single Source of Truth:** All replicas in the cell see the same repository state. If Replica A writes a WAL file, Replica B sees it immediately.
    2.  **Collaboration:** Any authentic replica can perform a backup, and any other replica can restore from it (within the same cell).

**Implications:**
1.  **ReadWriteMany (RWX) or S3 Required:** Since multiple pods (replicas) in the same cell need to mount the PVC simultaneously from potentially different nodes, the underlying storage class MUST support `ReadWriteMany` (NFS, EFS, etc.) **or** users should use S3 instead.
    - **With direct pod management (current architecture):** The operator creates pods **sequentially** (one at a time, gated on readiness). By the time the second pod is created, the shared backup volume is already attached to the first pod's node. The Kubernetes scheduler has no mechanism to detect this active RWO attachment when scheduling the second pod — it only checks PV `nodeAffinity` (AZ constraint), not active volume attachments. If the scheduler places the second pod on a different node, the `attachdetach-controller` fails with a `Multi-Attach error`. This is a known Kubernetes limitation: the scheduler does not prevent cross-node scheduling of pods referencing an already-bound RWO PVC via `spec.volumes[]`.
    - **With S3 backups:** The backup volume uses `EmptyDir` (local scratch only), so there is no shared PVC and no Multi-Attach issue. All pods connect to S3 independently. This is the recommended production backend.
2.  **Cell Isolation:** This shared PVC is **Cell-Local**. A backup created in `us-east-1a` (Cell A) is stored in Cell A's PVC. It is **NOT** available to Cell B (`us-east-1b`).
    - **Consequence:** You cannot failover to Cell B and restore from Cell A's filesystem backup.
    - **Solution:** Use **S3** for multi-cell clusters. S3 provides a single global repository accessible from all cells.

## Replica Selection Logic (Upstream Behavior)
To prevent performance degradation on the primary (Writer), the operator's **MultiAdmin** component explicitly selects a **replica** to perform the backup.
1. It scans the global topology for all cells.
2. It looks for a healthy **Replica** pooler in any cell.
3. It triggers the backup command on that specific replica.
4. **Note:** Since any replica in *any* cell might be chosen, all cells must have write access to the backup repository. This works natively with S3. For Filesystem backups, this reinforces the limitation that they are only viable for single-cell deployments (or multi-cell deployments where a shared network filesystem intersects all cells).
