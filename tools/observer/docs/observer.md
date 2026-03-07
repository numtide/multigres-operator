# Observer Reference

The observer runs a continuous loop (default interval: 10 seconds) performing 10 categories of health checks against a Multigres cluster. Every finding is emitted as a structured JSON log line and recorded as a Prometheus metric. A complete diagnostic snapshot is available via `GET /api/status` (see `architecture.md` for the JSON schema).

The observer is **read-only** — it never modifies any resource. It runs in the `multigres-operator` namespace and watches all namespaces for Multigres CRDs by default.

---

## Check Categories

### 1. Pod Health (`pod-health`)

**File:** `observer/pods.go`

Validates the health of every Multigres-managed pod in the cluster.

| Sub-check | What it detects | Threshold | Severity |
|-----------|----------------|-----------|----------|
| Phase validation | Pods in `Pending`, `CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull` | Pending >60s | error |
| Container readiness | Any container not Ready | Not ready >30s | error |
| Restart tracking | New container restarts since last cycle | 1 restart = warn, ≥3 in 5min = error | warn/error |
| OOMKill detection | Container terminated with reason `OOMKilled` | Immediate | error |
| Terminating stuck | Pod in `Terminating` state too long | >90s | error |
| Pod count validation | Pool/MultiOrch/MultiGateway/TopoServer pod counts vs spec | Mismatch | error |
| Operator pod health | Operator pod running with all containers ready | Not running | error |

**What it tracks across cycles:**
- `prevRestarts`: restart counts per container to detect new restarts
- `podPhaseSince`: when a pod entered its current phase (for timeout detection)

---

### 2. Resource Validation (`resource-validation`)

**File:** `observer/resources.go`

Validates the existence, ownership, and consistency of all Multigres-managed Kubernetes resources.

| Sub-check | What it detects | Severity |
|-----------|----------------|----------|
| Cluster → Cell ownership | Cells missing ownerReference to their MultigresCluster | error |
| Cluster → TableGroup ownership | TableGroups missing ownerReference | error |
| Cluster → TopoServer ownership | TopoServer missing ownerReference | error |
| MultiGateway Deployment | Missing or unexpected MultiGateway deployments per cell | error |
| MultiGateway Service | Missing MultiGateway services per cell | error |
| MultiOrch Deployment | Missing MultiOrch deployments per shard per cell | error |
| MultiOrch Service | Missing MultiOrch services per shard per cell | error |
| Pool StatefulSets | Missing StatefulSets per pool per cell | error |
| Pool headless Services | Missing headless services per pool per cell | error |
| Orphan detection | Resources with multigres labels but no ownerReference | warn |
| Stuck terminating | Multigres resources stuck in `Terminating` for >5min | error |

---

### 3. CRD Status Validation (`crd-status`)

**File:** `observer/status.go`

Validates the `.status` fields of all Multigres CRDs.

| Sub-check | What it detects | Threshold | Severity |
|-----------|----------------|-----------|----------|
| Phase regression | Cluster/Shard/Cell/TopoServer in `Degraded` or `Unknown` | >5min | error |
| Generation staleness | `observedGeneration ≠ generation` | >60s | error |
| PodRoles validation | Wrong number of primaries per pool per cell | 30s grace | error |
| PodRoles stale entries | PodRoles referencing pods that no longer exist | 30s grace | warn |
| ReadyReplicas accuracy | `readyReplicas` doesn't match actual pod count | Immediate | error |
| Shard readiness | `orchReady`/`poolsReady` booleans inconsistent | Immediate | error |
| Shard cells list | Missing or extra entries in `status.cells` | Immediate | error |
| ReadyForDeletion condition | Present on a shard that isn't being deleted | Immediate | warn |
| BackupHealthy condition | `BackupStale` (warn), `BackupFailed` (error) | Immediate | warn/error |

**Primary role validation details:**
- Exactly 1 primary expected per pool per cell
- 30-second grace period before flagging violations (failovers cause brief 0-or-2-primary windows)

---

### 4. Drain State Machine (`drain-state`)

**File:** `observer/drain.go`

Monitors the drain state machine on pool pods via the `drain.multigres.com/state` annotation.

| Sub-check | What it detects | Threshold | Severity |
|-----------|----------------|-----------|----------|
| Stuck in `requested` | Operator hasn't moved to `draining` | >30s | error |
| Stuck in `draining` | Drain hasn't completed | >5min | error |
| Stuck in `acknowledged` | Hasn't progressed to `ready-for-deletion` | >30s | error |
| Backward transition | State went backwards (e.g., `draining` → `requested`) | Immediate | fatal |
| Concurrent drains | Multiple pods draining simultaneously in same shard | Unless shard deleting | error |

**Valid state progression:** `requested` → `draining` → `acknowledged` → `ready-for-deletion`

---

### 5. Connectivity (`connectivity`)

**File:** `observer/connectivity.go`

Probes all Multigres service endpoints for TCP/HTTP/gRPC/SQL connectivity.

| Probe (check string) | Target | Port | Method | Severity |
|----------------------|--------|------|--------|----------|
| `multigateway-pg` | Service | 15432 | TCP connect | error |
| `multigateway-liveness` | Service | 15100 | `GET /live` | error |
| `multigateway-readiness` | Service | 15100 | `GET /ready` | warn/error |
| `sql-probe` | Service | 15432 | `SELECT 1` via pgx (simple protocol) | error |
| `multiorch-liveness` | Service | 15300 | `GET /live` | error |
| `multiorch-readiness` | Service | 15300 | `GET /ready` | warn/error |
| `multiorch-pooler-health` | Service | 15300 | `GET /debug/status` (HTML scrape) | error/fatal |
| `etcd-health` | Service | 2379 | `GET /health` | error |
| `multipooler-health` | Pod | 15200 | `GET /live` | error |
| `multipooler-readiness` | Pod | 15200 | `GET /ready` | warn/error |
| `multipooler-grpc-health` | Pod | 15270 | gRPC `Health/Check` (3s timeout) | error/warn |
| `operator-health` | Pod | 8081 | `GET /healthz` | error |
| `operator-readiness` | Pod | 8081 | `GET /readyz` | error |
| Readiness cross-check | All pods | — | Compare K8s Ready vs probe results | fatal/error |

**Latency tracking:** All probes measure and report latency. Alerts when >500ms.

**Readiness cross-check:** After all probes complete, the observer compares each pod's Kubernetes `Ready` condition against the observer's own probe results. If Kubernetes says `Ready=True` but our probes show the component is broken, a `fatal` finding is raised. This detects "lying" readiness endpoints — components that report healthy to Kubernetes while actually being non-functional (e.g., multipooler returning `/ready=200` while its gRPC server is hanging).

**MultiOrch pooler health:** Scrapes the `/debug/status` HTML page and looks for error indicators (`DeadlineExceeded`, `Unavailable`, `unhealthy`, `connection refused`). If all poolers show errors, reports `fatal`; if some do, reports `error`.

> **Note:** The gateway SQL probe uses PostgreSQL simple query protocol (`QueryExecModeSimpleProtocol`)
> instead of pgx's default extended protocol. The multigateway does not yet support the extended
> protocol's Describe step (fails with SQLSTATE MTD06).

---

### 6. Replication Health (`replication`)

**File:** `observer/replication.go`

SQL-based data-plane health checks. Connects directly to PostgreSQL on pool pods (port 5432). Requires `--enable-sql-probe=true` (default).

#### Primary Pod Checks

| Sub-check | Query | Threshold | Severity |
|-----------|-------|-----------|----------|
| Sync replication state | `pg_stat_replication` → `sync_state` | Async when sync configured | fatal |
| Truncated app name | `application_name` length == 63 | Immediate | fatal |
| Replication lag | `replay_lag`, `write_lag`, `pg_wal_lsn_diff` | >10s warn, >60s error | warn/error |
| Missing replicas | 0 replication connections but replicas expected | Immediate | error |
| Blocked writes | `BEGIN; CREATE TEMP TABLE; ROLLBACK` | Timeout | fatal |

#### Replica Pod Checks

| Sub-check | Query | What it means | Severity |
|-----------|-------|--------------|----------|
| WAL receiver down | `SELECT COUNT(*) FROM pg_stat_wal_receiver` | Replica not connected to primary | error |
| WAL receiver not streaming | `SELECT status FROM pg_stat_wal_receiver` | Receiver stuck in non-streaming state | warn |
| WAL replay paused | `SELECT pg_is_wal_replay_paused()` | Replay stuck, standby falling behind | warn |

#### Split-Brain Detection

| Sub-check | Query | What it means | Severity |
|-----------|-------|--------------|----------|
| Role mismatch | `SELECT pg_is_in_recovery()` vs `podRoles` | Pod's actual role doesn't match CRD | error |
| Multiple primaries | >1 pod reports `pg_is_in_recovery()=false` | Split-brain | fatal |

**Safety:** All SQL probes are read-only or immediately rolled back. The write probe uses `CREATE TEMP TABLE` + `ROLLBACK` — nothing persists.

---

### 7. Log Monitoring (`operator-logs` / `dataplane-logs`)

**File:** `observer/logs.go`

Tails logs from all Multigres containers each cycle (configurable via `--log-tail-lines`, default 100). Findings are emitted under two separate check names depending on the source.

**Operator log patterns** (check: `operator-logs` — scanned from the operator `manager` container):

| Pattern | Severity |
|---------|----------|
| `error reconciling` | error |
| `failed to` | error |
| `stuck in Terminating` | error |
| `topology error` | error |
| `status error` | error |
| `panic` | fatal |
| `runtime error` | fatal |
| `backup stale` | warn |
| `pod replaced` | warn |
| `config error` | warn |
| `expand PVC failed` | warn |

**Data plane log patterns** (check: `dataplane-logs` — scanned from multipooler, postgres, multigateway, multiorch, and toposerver containers):

| Pattern | Severity |
|---------|----------|
| `connection refused` | error |
| `connection reset` | error |
| `topology registration` | error |
| `replication error` | error |
| `FATAL` | error |
| `panic` | fatal |
| `OOM` | error |
| `out of memory` | error |

**How it works:**
- Uses `pods/log` API with `sinceSeconds` based on interval each cycle
- Aggregates matches per pattern per container (reports count + sample line)
- Pattern matching is case-insensitive

---

### 8. Event Monitoring (`events`)

**File:** `observer/events.go`

Watches Kubernetes events for all Warning events on Multigres resources.

**Operator events flagged:**

| Reason | Meaning | Severity |
|--------|---------|----------|
| `BackupStale` | Backup age exceeded 25h | warn |
| `ConfigError` | Invalid configuration detected | error |
| `ExpandPVCFailed` | PVC resize failed | error |
| `PodReplaced` | A drained pod was replaced | warn |
| `StatusError` | Status update failed | error |
| `StuckTerminating` | Pod stuck terminating >60s | error |
| `TopologyError` | etcd topology operation failed | error |

**Kubernetes events flagged:**

| Reason | Meaning | Severity |
|--------|---------|----------|
| `FailedScheduling` | Pod can't be scheduled | error |
| `FailedMount` | Volume mount failed | error |
| `Unhealthy` | Readiness/liveness probe failed | warn |
| `BackOff` | Container crash loop | error |
| `OOMKilling` | OOM kill from kernel | fatal |
| `EvictionThresholdMet` | Node under pressure | warn |

---

### 9. Topology Validation (`topology`)

**File:** `observer/topology.go`

Validates etcd topology state against Kubernetes CRDs. **Optional** — silently skipped if etcd is unreachable.

| Sub-check | What it detects | Severity |
|-----------|----------------|----------|
| Cell registration | Cells in CRD but not in etcd (or vice versa) | error |
| Database registration | Databases in spec but not in etcd | error |
| Pooler registration | Running pool pods not registered as poolers in etcd | error |
| Orphaned poolers | Pooler entries in etcd for pods that don't exist | warn |
| Drained poolers | Pods in `ready-for-deletion` still registered in etcd | error |

**When etcd is unreachable:** A single `warn` is emitted: `"topology validation skipped: etcd unreachable"`. No checks run. The observer never crashes due to etcd connectivity issues.

---

## SQL Queries Reference

The observer connects directly to PostgreSQL on pool pods (port 5432) to detect data-plane issues invisible to Kubernetes probes. This section documents every SQL query, how connections are made, what data is read, and what (if anything) is written.

### Connection Method

All connections use the `pgx` driver with this connection string:

```
host=<pod-IP> port=5432 user=postgres dbname=postgres connect_timeout=5 sslmode=disable
```

Connections are established directly to pod IPs (not via services) for reliability. Each connection has a 5-second timeout and is closed immediately after the check completes. The observer connects as the `postgres` superuser to access system views.

### Connectivity SQL Probe (`connectivity.go`)

This query runs against the **MultiGateway service** (port 15432), not directly on pool pods. It validates end-to-end SQL connectivity through the routing layer.

```sql
SELECT 1
```

- **Target:** MultiGateway service on port 15432
- **Purpose:** Validates that the full query path works (client → gateway → pooler → postgres → response)
- **Reads:** Nothing (the constant `1` is returned by the planner without touching any table)
- **Writes:** Nothing
- **Latency:** Measured and recorded to `multigres_observer_probe_latency_seconds{check="sql-probe"}`

---

### Primary Replication Queries (`replication.go`)

These queries run against **primary pool pods** on port 5432.

#### 1. Synchronous Standby Configuration

```sql
SHOW synchronous_standby_names
```

- **Purpose:** Determines if synchronous replication is configured
- **Reads:** PostgreSQL GUC `synchronous_standby_names`
- **Writes:** Nothing
- **Returns:** Empty string if async-only, or a value like `ANY 1 ("zone-a_my-pod-1")` if sync is configured
- **Used by:** If non-empty, any standby in `async` sync_state triggers a `fatal` finding (writes will block)

#### 2. Replication Status & Lag

```sql
SELECT application_name, sync_state, sync_priority,
    COALESCE(EXTRACT(EPOCH FROM replay_lag)::bigint, 0),
    COALESCE(EXTRACT(EPOCH FROM write_lag)::bigint, 0),
    COALESCE(pg_wal_lsn_diff(sent_lsn, replay_lsn)::bigint, 0)
FROM pg_stat_replication
```

- **Purpose:** Reads the replication state of all connected standbys from the primary's perspective
- **Reads:** `pg_stat_replication` system view (one row per connected standby)
- **Writes:** Nothing
- **Columns returned:**

| Column | Type | What it tells us |
|--------|------|-----------------|
| `application_name` | text | Standby identifier (set by the standby on connection). If exactly 63 chars, it's been truncated by `NAMEDATALEN` and sync matching may be broken |
| `sync_state` | text | `async`, `sync`, `potential`, or `quorum`. If `async` when sync is configured → fatal |
| `sync_priority` | int | Priority in synchronous group (0 = not a sync candidate) |
| `replay_lag` | interval → seconds | How far behind the standby's replay is. >10s = warn, >60s = error |
| `write_lag` | interval → seconds | How far behind the standby's write-ahead is |
| `pg_wal_lsn_diff(sent_lsn, replay_lsn)` | bigint (bytes) | Byte offset between what the primary sent and what the standby replayed |

**What it detects:**
- **Truncated `application_name`:** Names exactly 63 chars long indicate PostgreSQL's `NAMEDATALEN` limit truncated the name. This breaks `synchronous_standby_names` matching because the primary's replication view has the truncated name, but the config references the full name. Result: writes block indefinitely.
- **Async standbys with sync configured:** When `synchronous_standby_names` is set but a standby's `sync_state` is `async`, it means the primary can't find a matching sync standby. Writes will block waiting for acknowledgment from a standby that will never match.
- **Replication lag:** Large `replay_lag` means the standby is falling behind the primary, increasing data loss risk on failover.

#### 3. Write Probe

```sql
BEGIN;
CREATE TEMP TABLE IF NOT EXISTS _observer_write_probe (x int);
ROLLBACK;
```

- **Purpose:** Detect whether writes are blocked on the primary
- **Reads:** Nothing
- **Writes:** Creates a temporary table inside a transaction that is **always rolled back**. The temp table `_observer_write_probe` is session-scoped and would be dropped when the connection closes anyway, but the explicit `ROLLBACK` ensures nothing persists even transiently.
- **Why this works:** When synchronous replication is configured and no sync standby is connected, any write operation (`INSERT`, `UPDATE`, `CREATE TABLE`) blocks indefinitely waiting for the standby to acknowledge. This probe detects that condition with a 3-second timeout — if `CREATE TEMP TABLE` doesn't complete in 3 seconds, writes are blocked.
- **Safety:** The `ROLLBACK` is issued whether the `CREATE` succeeds or fails. Even if the connection is dropped before `ROLLBACK`, the temp table was created inside a transaction that never committed, so PostgreSQL discards it automatically. **Nothing is persisted to disk. No WAL records are generated that would be replicated.**

---

### Replica Queries (`replication.go`)

These queries run against **replica (standby) pool pods** on port 5432.

#### 4. WAL Receiver Count

```sql
SELECT COUNT(*) FROM pg_stat_wal_receiver
```

- **Purpose:** Check if the standby has an active WAL receiver (connection to the primary)
- **Reads:** `pg_stat_wal_receiver` system view (0 or 1 rows on a standby)
- **Writes:** Nothing
- **Returns:** `0` if no WAL receiver is running (standby is disconnected from primary), `1` if connected
- **Severity:** `error` when count is 0 — the replica is not receiving WAL from the primary and will fall behind

#### 5. WAL Receiver Status

```sql
SELECT status FROM pg_stat_wal_receiver LIMIT 1
```

- **Purpose:** Check the WAL receiver's connection state
- **Reads:** `status` column from `pg_stat_wal_receiver`
- **Writes:** Nothing
- **Expected value:** `streaming` (actively receiving WAL)
- **Other possible values:** `starting`, `waiting`, `backup`, `stopping`
- **Severity:** `warn` when status is not `streaming` — the receiver may be in a transient state or stuck

#### 6. WAL Replay Paused

```sql
SELECT pg_is_wal_replay_paused()
```

- **Purpose:** Check if WAL replay is paused on the standby
- **Reads:** Calls a built-in PostgreSQL function (no table access)
- **Writes:** Nothing
- **Returns:** `true` if replay is paused, `false` if replay is running
- **Why it matters:** If WAL replay is paused, the standby accepts WAL but doesn't apply it. The standby falls increasingly behind the primary. This can happen if someone manually paused replay (e.g., for point-in-time queries) and forgot to resume it.
- **Severity:** `warn` — may be intentional but should be investigated

---

### Split-Brain Queries (`replication.go`)

This query runs against **every pool pod** (both primary and replica) on port 5432.

#### 7. Recovery State

```sql
SELECT pg_is_in_recovery()
```

- **Purpose:** Determine whether PostgreSQL is running as a primary or standby
- **Reads:** Calls a built-in PostgreSQL function (no table access)
- **Writes:** Nothing
- **Returns:** `false` if the server is a primary (accepting writes), `true` if it's a standby (in recovery mode)
- **Cross-referenced with:** `shard.status.podRoles` from the Kubernetes CRD

**What it detects:**
- **Role mismatch:** If `pg_is_in_recovery()=false` (primary) but `podRoles` says `standby`, or vice versa — the CRD is out of sync with reality. Severity: `error`.
- **Split-brain:** If >1 pod returns `pg_is_in_recovery()=false`, multiple PostgreSQL instances think they're the primary. This is a critical data integrity issue. Severity: `fatal`.

---

### Safety Summary

| Aspect | Guarantee |
|--------|-----------|
| **Data modification** | Only the write probe creates anything, and it's always rolled back. All other queries are read-only system view queries. |
| **WAL generation** | The write probe's temp table + rollback does not generate WAL records that would be replicated to standbys. |
| **Disk persistence** | Nothing is ever committed. Temp tables are session-scoped and discarded on disconnect. |
| **Connection lifecycle** | Every connection is opened, used for ≤5s, and closed. No persistent connections. |
| **Failure isolation** | If a connection fails, the observer logs it and moves on. No retries, no cascading failures. |
| **Credential scope** | Connects as `postgres` superuser (required for `pg_stat_replication` access). Uses `sslmode=disable` since connections are pod-to-pod within the cluster network. |

---

## Useful Log Queries

Filter for specific checks or severity levels:

```bash
# Just fatals
kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator | grep '"fatal"'

# Just replication issues
kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator | grep '"replication"'

# Cycle summaries only
kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator | grep 'observer cycle complete'

# Split-brain
kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator | grep 'SPLIT-BRAIN'
```
