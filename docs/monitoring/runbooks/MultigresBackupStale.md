# MultigresBackupStale

## Meaning

The most recent completed backup for a shard is older than 24 hours (sustained for 30 minutes). The `multigres_operator_last_backup_age_seconds` gauge exceeds 86400.

## Impact

If a disaster occurs, the Recovery Point Objective (RPO) may be exceeded. WAL archiving may still be operational, but without a recent base backup, recovery times increase and the WAL chain grows unbounded.

## Investigation

```bash
# Check backup age metric
kubectl exec -n monitoring prometheus-0 -- promtool query instant \
  'multigres_operator_last_backup_age_seconds{shard="<shard-name>"}'

# Check the Shard status for last backup info
kubectl describe shard <shard-name> -n <namespace>

# Check pool pod logs for pgBackRest errors
kubectl logs -n <namespace> -l multigres.com/shard=<shard-name> --tail=100 | grep -i backrest

# Check if the backup PVC is full (filesystem backend)
kubectl exec -n <namespace> <pool-pod> -- df -h /backups

# Check S3 connectivity (S3 backend)
kubectl exec -n <namespace> <pool-pod> -- pgbackrest info
```

## Remediation

1. **pgBackRest errors** — Check pod logs for authentication, network, or storage errors. For S3, verify credentials and bucket permissions.
2. **Full backup PVC** — Expand the PVC or clean up old backups. Consider switching to S3 for production workloads.
3. **Pod not ready** — If the replica selected for backups is unhealthy, the backup cannot run. Resolve the pod health issue first (see `MultigresShardPoolDegraded`).
4. **Manual trigger** — If the automated backup schedule is misconfigured, manually trigger a backup via `pgbackrest backup` in a pool pod.
