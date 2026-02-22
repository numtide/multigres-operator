# Multigres Operator

The **[Multigres](https://github.com/multigres/multigres) Operator** is a Kubernetes operator for managing distributed, sharded PostgreSQL clusters across multiple failure domains (zones or regions). It provides a unified API to define the topology of your database system, handling the complex orchestration of `shards`, `cells` (failure domains), and `gateways`.

## Features
- **Global Cluster Management**: Single source of truth (`MultigresCluster`) for the entire database topology.
- **Automated Sharding**: Manages `TableGroups` and `Shards` as first-class citizens.
- **Failover & High Availability**: Orchestrates Primary/Standby failovers across defined Cells.
- **Template System**: Define configuration once (`CoreTemplate`, `CellTemplate`, `ShardTemplate`) and reuse it across the cluster.
- **Hierarchical Defaults**: Smart override logic allowing for global defaults, namespace defaults, and granular overrides.
- **Integrated Cert Management**: Built-in self-signed certificate generation and rotation for validatating webhooks, with optional support for `cert-manager`.

---

## Installation

### Prerequisites
- Kubernetes v1.25+

### Quick Start

Install the operator with built-in self-signed certificate management:

```bash
kubectl apply --server-side -f \
  https://github.com/numtide/multigres-operator/releases/latest/download/install.yaml
```

This deploys the operator into the `multigres-operator` namespace with:
- All CRDs (MultigresCluster, Cell, Shard, TableGroup, TopoServer, and templates)
- RBAC roles and bindings
- Mutating and Validating webhooks with self-signed certificates (auto-rotated)
- The operator Deployment
- Metrics endpoint

### With cert-manager

If you prefer external certificate management via [cert-manager](https://cert-manager.io/):

```bash
# 1. Install cert-manager (if not already present)
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.17.2/cert-manager.yaml
kubectl wait --for=condition=Available deployment --all -n cert-manager --timeout=120s

# 2. Install the operator
kubectl apply --server-side -f \
  https://github.com/numtide/multigres-operator/releases/latest/download/install-certmanager.yaml
```

### With observability stack

Install the operator alongside Prometheus, OpenTelemetry Collector, Tempo, and Grafana for metrics, tracing, and dashboards:

```bash
# 1. Install the Prometheus Operator (if not already present)
kubectl apply --server-side -f \
  https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.80.0/bundle.yaml
kubectl wait --for=condition=Available deployment/prometheus-operator -n default --timeout=120s

# 2. Install the operator with observability
kubectl apply --server-side -f \
  https://github.com/numtide/multigres-operator/releases/latest/download/install-observability.yaml
```

> [!NOTE]
> The bundled Prometheus, Tempo, Grafana, and OTel Collector are single-replica deployments with sane defaults intended for **evaluation and development**. They do not include HA, persistent storage, or authentication. For production observability, integrate the operator's metrics and traces with your existing monitoring infrastructure.

### Applying samples

Once the operator is running, try a sample cluster:

```bash
kubectl apply -f https://raw.githubusercontent.com/numtide/multigres-operator/main/config/samples/minimal.yaml
```

| Sample | Description |
| :--- | :--- |
| `config/samples/minimal.yaml` | The simplest possible cluster, relying entirely on system defaults. |
| `config/samples/templated-cluster.yaml` | A full cluster example using reusable templates. |
| `config/samples/templates/` | Individual `CoreTemplate`, `CellTemplate`, and `ShardTemplate` examples. |
| `config/samples/default-templates/` | Namespace-level default templates (named `default`). |
| `config/samples/overrides.yaml` | Advanced usage showing how to override specific fields on top of templates. |
| `config/samples/no-templates.yaml` | A verbose example where all configuration is defined inline. |
| `config/samples/external-etcd.yaml` | Connecting to an existing external Etcd cluster. |

### Local Development (Kind)

For local testing using Kind, we provide several helper commands:

| Command | Description |
| :--- | :--- |
| `make kind-deploy` | Deploy operator to local Kind cluster using self-signed certs (Default). |
| `make kind-deploy-certmanager` | Deploy operator to Kind, installing `cert-manager` for certificate handling. |
| `make kind-deploy-no-webhook` | Deploy operator to Kind with the webhook fully disabled. |
| `make kind-deploy-observability` | Deploy operator with full observability stack (Prometheus Operator, OTel Collector, Tempo, Grafana). |
| `make kind-portforward` | Port-forward Grafana (3000), Prometheus (9090), Tempo (3200) to localhost. Re-run if connection drops. |

---

## How it Works

The Multigres Operator follows a **Parent/Child** architecture. You, the user, manage the **Root** resource (`MultigresCluster`) and its shared **Templates**. The operator automatically creates and reconciles all necessary child resources (`Cells`, `TableGroups`, `Shards`, `TopoServers`) to match your desired state.

### Resource Hierarchy

```ascii
[MultigresCluster] 🚀 (Root CR - User Editable)
      │
      ├── 📍 Defines [TemplateDefaults] (Cluster-wide default templates)
      │
      ├── 🌍 [GlobalTopoServer] (Child CR) ← 📄 Uses [CoreTemplate] OR inline [spec]
      │
      ├── 🤖 MultiAdmin Resources ← 📄 Uses [CoreTemplate] OR inline [spec]
      │
      ├── 💠 [Cell] (Child CR) ← 📄 Uses [CellTemplate] OR inline [spec]
      │    │
      │    ├── 🚪 MultiGateway Resources
      │    └── 📡 [LocalTopoServer] (Child CR, optional)
      │
      └── 🗃️ [TableGroup] (Child CR)
           │
           └── 📦 [Shard] (Child CR) ← 📄 Uses [ShardTemplate] OR inline [spec]
                │
                ├── 🧠 MultiOrch Resources (Deployment/Pod)
                └── 🏊 Pools (StatefulSets for Postgres+MultiPooler)

📄 [CoreTemplate] (User-editable, scoped config)
   ├── globalTopoServer
   └── multiadmin

📄 [CellTemplate] (User-editable, scoped config)
   ├── multigateway
   └── localTopoServer (optional)

📄 [ShardTemplate] (User-editable, scoped config)
   ├── multiorch
   └── pools (postgres + multipooler)
```

**Important**:
*   **Only** `MultigresCluster`, `CoreTemplate`, `CellTemplate`, and `ShardTemplate` are meant to be edited by users.
*   Child resources (`Cell`, `TableGroup`, `Shard`, `TopoServer`) are **Read-Only**. Any manual changes to them will be immediately reverted by the operator to ensure the system stays in sync with the root configuration.

---

## Configuration & Defaults

The operator uses a **4-Level Override Chain** to resolve configuration for every component. This allows you to keep your `MultigresCluster` spec clean while maintaining full control when needed.

### 1. The Default Hierarchy

When determining the configuration for a component (e.g., a Shard), the operator looks for configuration in this order:

1.  **Inline Spec / Explicit Template Ref**: Defined directly on the component in the `MultigresCluster` YAML.
2.  **Cluster-Level Template Default**: Defined in `spec.templateDefaults` of the `MultigresCluster`.
3.  **Namespace-Level Default**: A template of the correct kind (e.g., `ShardTemplate`) named `"default"` in the same namespace.
4.  **Operator Hardcoded Defaults**: Fallback values built into the operator Webhook.

### 2. Templates and Overrides

Templates allow you to define standard configurations (e.g., "Standard High-Availability Cell"). You can then apply specific **overrides** on top of a template.

**Example: Using a Template with Overrides**
```yaml
spec:
  cells:
    - name: "us-east-1a"
      cellTemplate: "standard-ha-cell" # <--- Uses the template
      overrides:                       # <--- Patches specific fields
        multigateway:
          replicas: 5                  # <--- Overrides only the replica count
```

**Note on Overrides**: When using `overrides`, you must provide the complete struct for the section you are overriding if it's a pointer. For specific fields like resources, it's safer to ensure you provide the full context if the merge behavior isn't granular enough for your needs (currently, the resolver performs a deep merge).

### 3. Template Update Behavior

> [!WARNING]
> When a template (`CoreTemplate`, `CellTemplate`, or `ShardTemplate`) is updated, **all clusters using that template are reconciled immediately**. This means changes to a shared template propagate to every referencing cluster at once.

For production environments where you want controlled rollouts, consider **versioning templates by name**:

```yaml
# Instead of editing "standard-shard" in-place...
apiVersion: multigres.com/v1alpha1
kind: ShardTemplate
metadata:
  name: standard-shard-v2   # <--- New version = new resource
spec:
  # ... updated configuration
```

Then update each cluster's `templateRef` individually when ready:
```yaml
spec:
  templateDefaults:
    shardTemplate: "standard-shard-v2"  # <--- Opt-in to the new version
```

> [!NOTE]
> Avoid using `default`-named templates (the namespace-level fallback) in production if you need controlled rollouts. They cannot be versioned since any cluster without an explicit template reference will automatically use whichever template is named `default`.
>
> This mechanism may change in future versions. See [Implementation Notes](plans/phase-1/implementation-notes.md#template-update-propagation) for details on planned improvements.

### 4. PVC Deletion Policy

The operator supports fine-grained control over **Persistent Volume Claim (PVC) lifecycle management** for stateful components (TopoServers and Shard Pools). This allows you to decide whether PVCs should be automatically deleted or retained when resources are deleted or scaled down.

#### Policy Options

The `pvcDeletionPolicy` field has two settings:

- **`whenDeleted`**: Controls what happens to PVCs when the entire MultigresCluster (or a component like a TopoServer) is deleted.
  - `Retain` (default): PVCs are preserved for manual review and potential data recovery
  - `Delete`: PVCs are automatically deleted along with the cluster

- **`whenScaled`**: Controls what happens to PVCs when reducing the number of replicas (e.g., scaling from 3 pods down to 1 pod).
  - `Retain` (default): PVCs from scaled-down pods are kept for potential scale-up
  - `Delete`: PVCs are automatically deleted when pods are removed

#### Safe Defaults

**By default, the operator uses `Retain/Retain`** for maximum data safety. This means:
- Deleting a cluster will **not** delete your data volumes
- Scaling down a StatefulSet will **not** delete the PVCs from removed pods

This is a deliberate choice to prevent accidental data loss.

#### Where to Set the Policy

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

#### Templates and PVC Policy

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

#### Important Caveats

⚠️ **Data Loss Risk**: Setting `whenDeleted: Delete` means **permanent data loss** when the cluster is deleted. Use this only for:
- Development/testing environments
- Ephemeral clusters
- Scenarios where data is backed up externally

⚠️ **Replica Scale-Down Behavior**: Setting `whenScaled: Delete` will **immediately delete PVCs** when reducing the number of replicas (pod count). If you scale the replica count back up, new pods will start with **empty volumes**. This is useful for:
- Reducing storage costs in non-production environments
- Stateless-like workloads where data is ephemeral

**Note**: This does NOT affect storage size. Changing PVC storage capacity is a separate operation and is not controlled by this policy.

✅ **Production Recommendation**: For production clusters, use the default `Retain/Retain` policy and implement proper backup/restore procedures.


---

## Backup & Restore

The operator integrates **pgBackRest** to handle automated backups, WAL archiving, and point-in-time recovery (PITR). Backup configuration is fully declarative and propagates from the Cluster level down to individual Shards.

### Architecture

Every Shard in the cluster has its own independent backup repository.
- **Replica-Based Backups:** To avoid impacting the primary's performance, backups are always performed by a **replica**. The operator's MultiAdmin component selects a healthy replica (typically in the primary zone/cell) to execute the backup.
- **Universal Availability:** While only one replica performs the backup, **all replicas** (current and future) need access to the backup repository to:
  1.  Bootstrap new replicas (via `pgbackrest restore`).
  2.  Perform Point-in-Time Recovery (PITR).
  3.  Catch up if they fall too far behind (WAL replay).

### Supported Storage Backends

#### 1. S3 (Recommended for Production)

S3 (or any S3-compatible object storage) is the **only supported method for multi-cell / multi-zone clusters**.
- **Why:** All replicas across all failure domains (zones/regions) can access the same S3 bucket.
- **Behavior:** The operator configures all pods to read/write to the specified bucket and path.

```yaml
spec:
  backup:
    type: s3
    s3:
      bucket: my-database-backups
      region: us-east-1
      keyPrefix: prod/cluster-1
      useEnvCredentials: true # Uses AWS_ACCESS_KEY_ID from env
```

#### 2. Filesystem (Development / Single-Node Only)

The `filesystem` backend stores backups on a Persistent Volume Claim (PVC).
- **Architecture:** The operator creates **One Shared PVC per Shard per Cell**.
- **Naming:** `backup-data-{cluster}-{db}-{tg}-{shard}-{cell}`.
- **Constraint:** All replicas in a specific Cell mount the *same* PVC.

> [!WARNING]
> **CRITICAL LIMITATION:** Filesystem backups are **Cell-Local**.
> A backup taken by a replica in `zone-a` is stored in `zone-a`'s PVC. Replicas in `zone-b` have their own empty PVC and **cannot see or restore** from `zone-a`'s backups.
>
> **Do not use `filesystem` backups for multi-cell clusters** unless you understand that cross-cell failover will result in a split-brain backup state.

**ReadWriteMany (RWX) Requirement:**
If you have multiple replicas in the same Cell (e.g., `replicasPerCell: 3`), they must all mount the same PVC simultaneously.
- **Option A (Recommended):** Use a StorageClass that supports `ReadWriteMany` (e.g., NFS, EFS, CephFS).
- **Option B (Dev/Test):** If using standard block storage (RWO), all replicas must be on the **same node**.

> [!CAUTION]
> **Silent HA Loss with RWO:** If your StorageClass uses `WaitForFirstConsumer` binding (standard for EBS/gp2/gp3), Kubernetes will **automatically co-locate all replicas on the same node** to satisfy the RWO constraint. The cluster will appear healthy, but all replicas are on a single node — if that node fails, you lose all replicas simultaneously. Use S3 or RWX storage for production to ensure replicas spread across nodes.

```yaml
spec:
  backup:
    type: filesystem
    filesystem:
      path: /backups
      storage:
        size: 10Gi
        storageClassName: "nfs-client" # Requires RWX support
```

### pgBackRest TLS Certificates

pgBackRest uses TLS for secure inter-node communication between replicas in a shard. The operator supports two modes for certificate provisioning:

#### Auto-Generated Certificates (Default)

When no `pgbackrestTLS` configuration is specified, the operator automatically generates and rotates a CA and server certificate per Shard using the built-in `pkg/cert` module. No user action is required.

#### User-Provided Certificates (cert-manager)

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

## Observability

The operator ships with built-in support for **metrics**, **alerting**, **distributed tracing**, and **structured logging**.

### Metrics

Metrics are served via the standard controller-runtime Prometheus endpoint. Set `--metrics-bind-address=:8080` (or any port) to enable it.

The operator exposes two classes of metrics:

**Framework Metrics** (provided automatically by controller-runtime):
- `controller_runtime_reconcile_total` — total reconcile count per controller
- `controller_runtime_reconcile_errors_total` — reconcile error rate
- `controller_runtime_reconcile_time_seconds` — reconcile latency histogram
- `workqueue_depth` — work queue backlog

**Operator-Specific Metrics**:

| Metric | Type | Labels | Description |
|:---|:---|:---|:---|
| `multigres_operator_cluster_info` | Gauge | `name`, `namespace`, `phase` | Cluster phase tracking (always 1) |
| `multigres_operator_cluster_cells_total` | Gauge | `cluster`, `namespace` | Cell count |
| `multigres_operator_cluster_shards_total` | Gauge | `cluster`, `namespace` | Shard count |
| `multigres_operator_cell_gateway_replicas` | Gauge | `cell`, `namespace`, `state` | Gateway ready/desired replicas |
| `multigres_operator_shard_pool_replicas` | Gauge | `shard`, `pool`, `namespace`, `state` | Pool ready/desired replicas |
| `multigres_operator_toposerver_replicas` | Gauge | `name`, `namespace`, `state` | TopoServer ready/desired replicas |
| `multigres_operator_webhook_request_total` | Counter | `operation`, `resource`, `result` | Webhook admission request count |
| `multigres_operator_webhook_request_duration_seconds` | Histogram | `operation`, `resource` | Webhook latency |

### Alerts

Pre-configured PrometheusRule alerts are provided in `config/monitoring/prometheus-rules.yaml`. Apply them to a Prometheus Operator installation:

```bash
kubectl apply -f config/monitoring/prometheus-rules.yaml
```

| Alert | Severity | Fires When |
|:---|:---:|:---|
| [`MultigresClusterReconcileErrors`](docs/monitoring/runbooks/MultigresClusterReconcileErrors.md) | warning | Sustained non-zero reconcile error rate (5m) |
| [`MultigresClusterDegraded`](docs/monitoring/runbooks/MultigresClusterDegraded.md) | warning | Cluster phase ≠ "Healthy" for >10m |
| [`MultigresCellGatewayUnavailable`](docs/monitoring/runbooks/MultigresCellGatewayUnavailable.md) | critical | Zero ready gateway replicas in a cell (5m) |
| [`MultigresShardPoolDegraded`](docs/monitoring/runbooks/MultigresShardPoolDegraded.md) | warning | Ready < desired replicas for >10m |
| [`MultigresWebhookErrors`](docs/monitoring/runbooks/MultigresWebhookErrors.md) | warning | Webhook returning errors (5m) |
| [`MultigresReconcileSlow`](docs/monitoring/runbooks/MultigresReconcileSlow.md) | warning | p99 reconcile latency >30s (5m) |
| [`MultigresControllerSaturated`](docs/monitoring/runbooks/MultigresControllerSaturated.md) | warning | Work queue depth >50 for >10m |

Each alert links to a dedicated runbook with investigation steps, PromQL queries, and remediation actions.

### Grafana Dashboards

Two Grafana dashboards are included in `config/monitoring/`:

- **Operator Dashboard** (`grafana-dashboard-operator.json`) — reconcile rates, error rates, latencies, queue depth, and webhook performance.
- **Cluster Dashboard** (`grafana-dashboard-cluster.json`) — per-cluster topology (cells, shards), replica health, and phase tracking.

Import via the Grafana dashboards ConfigMap:
```bash
kubectl apply -f config/monitoring/grafana-dashboards.yaml
```

### Local Development

For local development, the observability overlay in `config/deploy-observability/` deploys the OTel Collector, Prometheus (via the Prometheus Operator), Tempo, and Grafana as separate pods. Both dashboards and datasources are pre-provisioned.

```bash
make kind-deploy-observability
make kind-portforward
```

This deploys the operator with tracing enabled and opens port-forwards to:

| Service | URL |
| :--- | :--- |
| Grafana | [http://localhost:3000](http://localhost:3000) |
| Prometheus | [http://localhost:9090](http://localhost:9090) |
| Tempo | [http://localhost:3200](http://localhost:3200) |

**Metrics collection:** The operator and data-plane components use different metric collection models:

| Component | Metric Model | How it works |
| :--- | :--- | :--- |
| **Operator** | **Pull** (Prometheus scrape) | Prometheus scrapes the operator's `/metrics` endpoint via controller-runtime's built-in Prometheus integration |
| **Data plane** (multiorch, multipooler, etc.) | **Push** (OTLP) | Multigres binaries push metrics via OpenTelemetry to the configured OTLP endpoint |

The OTel Collector receives all pushed OTLP signals from the data plane and routes them: **traces → Tempo**, **metrics → Prometheus** (via its OTLP receiver). This is necessary because multigres components send all signals to a single OTLP endpoint and cannot split them by signal type.

### Distributed Tracing

The operator supports **OpenTelemetry distributed tracing** via OTLP. Tracing is **disabled by default** and incurs zero overhead when off.

**Enabling tracing:** Set a single environment variable on the operator Deployment:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector.monitoring.svc:4318"  # OTel Collector or Tempo
```

The endpoint must speak **OTLP** (HTTP or gRPC) — this can be an OpenTelemetry Collector, Grafana Tempo, Jaeger, or any compatible backend.

**What gets traced:**
- Every controller reconciliation (MultigresCluster, Cell, Shard, TableGroup, TopoServer)
- Sub-operations within a reconcile (ReconcileCells, UpdateStatus, PopulateDefaults, etc.)
- Webhook admission handling (defaulting and validation)
- Webhook-to-reconcile trace propagation: the defaulter webhook injects a trace context into cluster annotations so the first reconciliation appears as a child span of the webhook trace

**Additional OTel configuration:** The operator respects all standard [OTel environment variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/) including `OTEL_TRACES_SAMPLER`, `OTEL_EXPORTER_OTLP_INSECURE`, and `OTEL_SERVICE_NAME`.

### Structured Logging

The operator uses structured JSON logging (`zap` via controller-runtime). When tracing is enabled, every log line within a traced operation automatically includes `trace_id` and `span_id` fields, enabling **log-trace correlation** — click a log line in Grafana Loki to jump directly to the associated trace.

**Log level configuration:** The operator accepts standard controller-runtime zap flags on its command line:

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--zap-devel` | `true` | Development mode preset (see table below) |
| `--zap-log-level` | depends on mode | Log verbosity: `debug`, `info`, `error`, or an integer (0=debug, 1=info, 2=error) |
| `--zap-encoder` | depends on mode | Log format: `console` or `json` |
| `--zap-stacktrace-level` | depends on mode | Minimum level that triggers stacktraces |

`--zap-devel` is a **mode** that sets multiple defaults at once. `--zap-log-level` overrides the mode's default level when specified explicitly:

| Setting | `--zap-devel=true` (default) | `--zap-devel=false` (production) |
| :--- | :--- | :--- |
| Default log level | `debug` | `info` |
| Encoder | `console` (human-readable) | `json` |
| Stacktraces from | `warn` | `error` |

To change the log level in a deployed operator, add `args` to the manager container:

```yaml
spec:
  template:
    spec:
      containers:
        - name: manager
          args:
            - --zap-devel=false       # Production mode (JSON, info level default)
            - --zap-log-level=info    # Explicit level (overrides mode default)
```

> [!NOTE]
> The default build ships with `Development: true`, which sets the default level to `debug` and uses the human-readable console encoder. For production deployments, set `--zap-devel=false` to switch to JSON encoding and `info`-level logging.

---

## Webhook & Certificate Management

The operator includes a Mutating and Validating Webhook to enforce defaults and data integrity.

### Automatic Certificate Management (Default)
By default, the operator manages its own TLS certificates using the generic `pkg/cert` module. This implements a **Split-Secret PKI** architecture:

1.  **Bootstrap**: On startup, the cert rotator generates a self-signed Root CA (ECDSA P-256) and a Server Certificate, storing them in two separate Kubernetes Secrets.
2.  **CA Bundle Injection**: A post-reconcile hook automatically patches the `MutatingWebhookConfiguration` and `ValidatingWebhookConfiguration` with the CA bundle.
3.  **Rotation**: A background loop checks certificates hourly. Certs nearing expiry (or signed by a rotated CA) are automatically renewed without downtime.
4.  **Owner References**: Both secrets are owned by the operator Deployment, so they are garbage-collected on uninstall.

### Using external Cert-Manager
If you prefer to use `cert-manager` or another external tool, deploy using the cert-manager overlay (`install-certmanager.yaml`). This overlay:

1.  Disables the internal cert rotator via the `--webhook-use-internal-certs=false` flag.
2.  Creates a `Certificate` and `ClusterIssuer` resource for cert-manager to manage.
3.  Mounts the cert-manager-provisioned secret to `/var/run/secrets/webhook`.

---

## Operator Flags

You can customize the operator's behavior by passing flags to the binary (or editing the Deployment args).

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--webhook-enable` | `true` | Enable the admission webhook server. |
| `--webhook-cert-dir` | `/var/run/secrets/webhook` | Directory to read/write webhook certificates. |
| `--webhook-service-name` | `multigres-operator-webhook-service` | Name of the Service pointing to the webhook. |
| `--webhook-service-namespace`| *Current Namespace* | Namespace of the webhook service. |
| `--metrics-bind-address` | `"0"` | Address for metrics (set to `:8080` to enable). |
| `--leader-elect` | `false` | Enable leader election (recommended for HA deployments). |

---

## Constraints & Limits (v1alpha1)

Please be aware of the following constraints in the current version:

*   **Database Limit**: Only **1** database is supported per cluster. It must be named `postgres` and marked `default: true`.
*   **Shard Naming**: Shards currently must be named `0-inf` - this is a limitation of the current implementation of Multigres.
*   **Naming Lengths**:
    *   **TableGroup Names**: If the combined name (`cluster-db-tg`) exceeds **28 characters**, the operator automatically hashes the database and tablegroup names to ensure that the resulting child resource names (Shards, Pods, StatefulSets) stay within Kubernetes limits (63 chars).
    *   **Cluster Name**: Recommended to be under **20 characters** to ensure that even with hashing, suffixes fit comfortably.
*   **Immutable Fields**: Some fields like `zone` and `region` in Cell definitions are immutable after creation.
