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
- `cert-manager` (Optional, if using external certificate management)

### Quick Start
To install the operator with default settings:

```bash
# Install CRDs
make install

# Deploy Operator to the cluster (uses your current kubeconfig context)
make deploy
```

#### Local Development (Kind)
For local testing using Kind, we provide several helper commands:

| Command | Description |
| :--- | :--- |
| `make kind-deploy` | Deploy operator to local Kind cluster using self-signed certs (Default). |
| `make kind-deploy-certmanager` | Deploy operator to Kind, installing `cert-manager` for certificate handling. |
| `make kind-deploy-no-webhook` | Deploy operator to Kind with the webhook fully disabled. |

### Using Sample Configurations
We provide a set of samples to get you started quickly:

| Sample | Description |
| :--- | :--- |
| `config/samples/minimal.yaml` | A minimal startup cluster for testing. |
| `config/samples/templated-cluster.yaml` | A full cluster example using templates. |
| `config/samples/standard-templates.yaml` | Examples of Core, Cell, and Shard templates. |

To apply a sample:
```bash
kubectl apply -f config/samples/minimal.yaml
```

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

### 3. PVC Deletion Policy

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

### Distributed Tracing

The operator supports **OpenTelemetry distributed tracing** via OTLP gRPC. Tracing is **disabled by default** and incurs zero overhead when off.

**Enabling tracing:** Set a single environment variable on the operator Deployment:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "tempo.observability.svc.cluster.local:4317"  # or your OTel collector
```

The endpoint must speak **OTLP gRPC** — this can be an OpenTelemetry Collector, Grafana Tempo, Jaeger, or any compatible backend.

**What gets traced:**
- Every controller reconciliation (MultigresCluster, Cell, Shard, TableGroup, TopoServer)
- Sub-operations within a reconcile (ReconcileCells, UpdateStatus, PopulateDefaults, etc.)
- Webhook admission handling (defaulting and validation)
- Webhook-to-reconcile trace propagation: the defaulter webhook injects a trace context into cluster annotations so the first reconciliation appears as a child span of the webhook trace

**Additional OTel configuration:** The operator respects all standard [OTel environment variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/) including `OTEL_TRACES_SAMPLER`, `OTEL_EXPORTER_OTLP_INSECURE`, and `OTEL_SERVICE_NAME`.

### Structured Logging

The operator uses structured JSON logging (`zap` via controller-runtime). When tracing is enabled, every log line within a traced operation automatically includes `trace_id` and `span_id` fields, enabling **log-trace correlation** — click a log line in Grafana Loki to jump directly to the associated trace.

---

## Webhook & Certificate Management

The operator includes a Mutating and Validating Webhook to enforce defaults and data integrity.

### Automatic Certificate Management (Default)
By default, the operator manages its own certificates.
1.  **Bootstrap**: On startup, it checks the directory `/var/run/secrets/webhook` for existing `tls.crt` and `tls.key` files.
2.  **Generation**: If none are found, it generates a Self-Signed CA and a Server Certificate.
3.  **Patching**: It automatically patches the `MutatingWebhookConfiguration` and `ValidatingWebhookConfiguration` in the cluster with the new CA Bundle.
4.  **Rotation**: Certificates are automatically rotated (default: every 30 days) without downtime.

### Using external Cert-Manager
If you prefer to use `cert-manager` or another external tool, you must mount the certificates to the operator pod.

1.  **Configuration**: Mount a secret containing `tls.crt` and `tls.key` to `/var/run/secrets/webhook`.
2.  **Behavior**: The operator detects the files on disk and **disables** its internal rotation logic.

**Example Patch to Mount External Certs:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multigres-operator-controller-manager
  namespace: multigres-system
spec:
  template:
    spec:
      containers:
      - name: manager
        volumeMounts:
        - mountPath: /var/run/secrets/webhook
          name: cert-volume
          readOnly: true
      volumes:
      - name: cert-volume
        secret:
          defaultMode: 420
          secretName: my-cert-manager-secret # Your secret name
```

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
