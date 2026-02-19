---
title: Support S3 based backup
state: draft
tags: [backup, s3, pgbackrest, api-change]
---

# Summary

Expose the upstream `pgctld` S3 backup capabilities through the operator's CRD API, so users can back up PostgreSQL data to S3-compatible object storage instead of only local PVCs.

# Motivation

The operator currently hardcodes filesystem-based backups using PVCs. This requires a shared `ReadWriteMany` PVC across all pods in a pool, which is fragile in practice -- RWX storage classes are not universally available, behave inconsistently across providers, and add a failure mode that has nothing to do with the database itself.

In cloud environments (especially AWS), the natural backup target is S3. It provides a single, reliable destination that every pod can write to independently without coordinating through a shared filesystem. It also avoids provisioning storage upfront and scales without intervention.

The upstream `pgctld` already accepts S3 configuration through command-line flags (`--backup-type=s3`, `--backup-bucket`, `--backup-region`, `--backup-endpoint`, `--backup-key-prefix`, `--backup-use-env-credentials`). It generates the `pgbackrest.conf` at runtime from these flags. The operator just needs to plumb this configuration through.

## Non-Goals

- Backup scheduling, restore automation, or backup status reporting (future work)
- Multipooler `--pgbackrest-stanza` integration (deferred until upstream stabilizes)
- Non-S3 cloud storage backends (Azure Blob, GCS)

# Proposal

Replace the existing `BackupStorage` field in `PoolSpec` with a new `BackupConfig` struct that discriminates between S3 and filesystem backends. This is a breaking API change, which is acceptable since we're pre-v1.

When `BackupConfig` is nil, the operator defaults to filesystem backups with a 10Gi PVC, preserving the current behavior for users who don't specify anything.

## API Shape

`BackupConfig` has a `type` discriminator (`s3` or `filesystem`) and type-specific sub-structs:

- **`type: filesystem`**: Uses the existing `StorageSpec` (size, class, access modes) to create a shared backup PVC per pool/cell. This is what `BackupStorage` does today, just nested under the new structure.
- **`type: s3`**: Configures bucket, region, endpoint (for MinIO/Ceph), key prefix, and credentials. No PVC is created.

### Credential Strategies for S3

Two modes, kept simple:

1. **Secret reference** (`credentialsSecretName`): User creates a Secret with `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` keys. Operator injects them as env vars via `SecretKeyRef`. This is the recommended approach.
2. **Environment credentials** (`useEnvCredentials: true`): Operator passes `--backup-use-env-credentials` to pgctld, which relies on IAM roles or IRSA. No secret needed.

These are mutually exclusive. Inline credentials in the CRD spec are intentionally not supported to avoid storing secrets in etcd.

## User-Facing Examples

S3 with a credentials secret:
```yaml
pools:
  primary:
    backupConfig:
      type: s3
      s3:
        bucket: my-pgbackrest-backups
        region: us-east-1
        credentialsSecretName: s3-backup-creds
```

S3 with IAM/IRSA:
```yaml
pools:
  primary:
    backupConfig:
      type: s3
      s3:
        bucket: my-pgbackrest-backups
        region: us-east-1
        useEnvCredentials: true
```

S3 with MinIO (local dev):
```yaml
pools:
  primary:
    backupConfig:
      type: s3
      s3:
        bucket: pgbackrest
        endpoint: http://minio.default.svc:9000
        credentialsSecretName: minio-creds
```

Explicit filesystem:
```yaml
pools:
  primary:
    backupConfig:
      type: filesystem
      storage:
        size: 50Gi
        class: fast-ssd
```

Default (no backupConfig -- filesystem with 10Gi PVC, same as today):
```yaml
pools:
  primary:
    storage:
      size: 20Gi
```

# Design Details

## How Configuration Flows

1. **API layer**: `BackupConfig` is defined as a new type in the shared types file and added to `PoolSpec`, replacing `BackupStorage`.
2. **Container building**: The pgctld container args are extended with backup flags based on the config type. For S3, AWS credential env vars are injected from the referenced secret. For filesystem, the existing backup PVC is volume-mounted.
3. **StatefulSet building**: The backup volume is only added to the pod spec when using filesystem backups. S3 backups don't need a local volume.
4. **Controller reconciliation**: Backup PVC creation is skipped when using S3. The reconcile loop checks the backup type before calling the PVC reconciler.

The key invariant is: **S3 backups produce no PVC and no backup volume mount; filesystem backups produce both.** Everything else (data volume, socket dir, pg_hba configmap) remains unchanged.

## Default Behavior

When `BackupConfig` is nil, the operator synthesizes a default filesystem config with a 10Gi PVC. This preserves backward compatibility for users who never set `BackupStorage` -- the only breaking change is for users who explicitly used that field, who need to move their size/class/accessModes under `backupConfig.storage`.

## What Changes in the Upstream Contract

Nothing. The operator is purely passing through flags that pgctld already supports. pgctld handles pgbackrest configuration generation internally. The operator doesn't need to know about pgbackrest.conf, stanzas, or backup internals.

# Drawbacks

**Breaking API change**: Users who set `backupStorage` must migrate to `backupConfig.storage`. Since we're pre-v1 and the migration is a straightforward YAML restructure with no data impact (existing PVCs remain), this is acceptable.

# Alternatives Considered

**Keep `BackupStorage`, add a separate S3 field**: Avoids breaking changes but creates ambiguity when both are set. Rejected for API cleanliness.

**Support inline credentials in the CRD spec**: More convenient for development but stores secrets in etcd in plaintext. Rejected -- users can create a Secret in one extra step, and IRSA/IAM covers the zero-secret case.

# Infrastructure Needed

- MinIO deployment for local S3 testing in Kind

# References

- Upstream pgctld `go/cmd/pgctld/command/server.go`: backup flag definitions
- Upstream `Dockerfile.pgctld`: installs pgbackrest from PostgreSQL APT repo
- [pgBackRest S3 documentation](https://pgbackrest.org/user-guide.html#repo/s3)
