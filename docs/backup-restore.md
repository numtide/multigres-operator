# Backup & Restore

The operator integrates **pgBackRest** to handle automated backups, WAL archiving, and point-in-time recovery (PITR). Backup configuration is fully declarative and propagates from the Cluster level down to individual Shards. These features are part of the operator's [Level III (Full Lifecycle)](operator-capability-levels.md#level-3-full-lifecycle) capabilities.

## Architecture

Every Shard in the cluster has its own independent backup repository.
- **Replica-Based Backups:** To avoid impacting the primary's performance, backups are always performed by a **replica**. The operator's MultiAdmin component selects a healthy replica (typically in the primary zone/cell) to execute the backup.
- **Universal Availability:** While only one replica performs the backup, **all replicas** (current and future) need access to the backup repository to:
  1.  Bootstrap new replicas (via `pgbackrest restore`).
  2.  Perform Point-in-Time Recovery (PITR).
  3.  Catch up if they fall too far behind (WAL replay).

## Supported Storage Backends

### 1. S3 (Recommended for Production)

S3 (or any S3-compatible object storage) is the **only supported method for multi-cell / multi-zone clusters**.
- **Why:** All replicas across all failure domains (zones/regions) can access the same S3 bucket.
- **Behavior:** The operator configures all pods to read/write to the specified bucket and path.

**S3 Credential Options** (mutually exclusive):

| Option | Field | Description |
|--------|-------|-------------|
| **IRSA** (recommended for EKS) | `serviceAccountName` | User creates a ServiceAccount annotated with `eks.amazonaws.com/role-arn`. The EKS pod identity webhook injects OIDC tokens automatically. |
| **Static credentials** | `credentialsSecret` + `useEnvCredentials: true` | Operator injects `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` from a K8s Secret. |
| **EC2 instance metadata** | *(none)* | Default fallback — uses the node's IAM instance profile. |

```yaml
spec:
  backup:
    type: s3
    s3:
      bucket: my-database-backups
      region: us-east-1
      endpoint: https://s3.us-east-1.amazonaws.com  # Optional, for S3-compatible stores (MinIO, etc.)
      keyPrefix: prod/cluster-1

      # Option 1: IRSA (recommended for EKS)
      # User creates the SA externally with eks.amazonaws.com/role-arn annotation
      serviceAccountName: "multigres-backup"

      # Option 2: Static credentials from a Secret
      # (mutually exclusive with serviceAccountName)
      # credentialsSecret: "my-aws-secret"
      # useEnvCredentials: true

      # Option 3: EC2 instance metadata (default, no fields needed)
```

### 2. Filesystem (Development / Single-Node Only)

The `filesystem` backend stores backups on a Persistent Volume Claim (PVC).
- **Architecture:** The operator creates **One Shared PVC per Shard per Cell**.
- **Naming:** `backup-data-{cluster}-{db}-{tg}-{shard}-{cell}`.
- **Constraint:** All replicas in a specific Cell mount the *same* PVC.

> [!WARNING]
> **CRITICAL LIMITATION:** Filesystem backups are **Cell-Local**.
> A backup taken by a replica in `zone-a` is stored in `zone-a`'s PVC. Replicas in `zone-b` have their own empty PVC and **cannot see or restore** from `zone-a`'s backups.
>
> **Do not use `filesystem` backups for multi-cell clusters** unless you understand that cross-cell failover will result in a split-brain backup state.

**ReadWriteMany (RWX) or S3 Required for Multi-Replica Cells:**
If you have multiple replicas in the same Cell (e.g., `replicasPerCell: 2`), they must all access the same backup repository. Standard block storage (EBS gp2/gp3) uses `ReadWriteOnce` (RWO) and **cannot be attached to multiple nodes simultaneously**.

> [!CAUTION]
> **Multi-Attach Failure with RWO Storage:** The operator creates pool pods **sequentially** (one at a time, waiting for readiness). By the time the second pod is created, the shared backup EBS volume is already attached to the first pod's node. The Kubernetes scheduler has no mechanism to detect this active RWO attachment when scheduling the second pod, so it may place it on a different node. The `attachdetach-controller` then fails with a `Multi-Attach error` because EBS volumes physically cannot attach to two nodes. This is a known Kubernetes limitation — the scheduler does not prevent cross-node scheduling of pods referencing an already-bound RWO PVC via `spec.volumes[]`.
>
> **Note:** The previous StatefulSet-based architecture (v0.2.6) avoided this because `ParallelPodManagement` submitted all pods to the scheduler simultaneously. With `WaitForFirstConsumer`, the first pod to be processed annotated the unbound PVC with `volume.kubernetes.io/selected-node`, constraining all other pods to the same AZ. If only one node existed per AZ, all pods landed on the same node deterministically — but this was a **silent HA loss**, not a solution.

**For multi-replica cells, use one of these backends:**
- **S3 (Recommended):** No shared PVC needed — each pod writes to S3 independently via an `EmptyDir` scratch volume.
- **RWX Storage:** Use a StorageClass that supports `ReadWriteMany` (e.g., NFS, EFS, CephFS) so multiple pods can mount the backup PVC from different nodes.

```yaml
spec:
  backup:
    type: filesystem
    filesystem:
      path: /backups
      storage:
        size: 10Gi
        class: "nfs-client" # Requires RWX support
```

## pgBackRest TLS Certificates

pgBackRest uses TLS for secure inter-node communication between replicas in a shard. The operator supports two modes for certificate provisioning:

### Auto-Generated Certificates (Default)

When no `pgbackrestTLS` configuration is specified, the operator automatically generates and rotates a CA and server certificate per Shard using the built-in `pkg/cert` module. No user action is required.

### User-Provided Certificates (cert-manager)

To use certificates from [cert-manager](https://cert-manager.io/) or any external PKI, provide the Secret name in the backup configuration:

```yaml
spec:
  backup:
    type: filesystem
    filesystem:
      path: /backups
      storage:
        size: 10Gi
    pgbackrestTLS:
      secretName: my-pgbackrest-certs  # Must contain ca.crt, tls.crt, tls.key
```

The referenced Secret must contain three keys: `ca.crt`, `tls.crt`, and `tls.key`. This is directly compatible with cert-manager's default Certificate output:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: pgbackrest-tls
spec:
  secretName: my-pgbackrest-certs
  commonName: pgbackrest
  usages: [server auth, client auth]
  issuerRef:
    name: my-issuer
    kind: Issuer
```

> [!NOTE]
> The operator internally renames `tls.crt` → `pgbackrest.crt` and `tls.key` → `pgbackrest.key` via projected volumes to match upstream pgBackRest expectations. Users do not need to perform any manual key renaming.
