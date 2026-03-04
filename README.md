# Multigres Operator

The **[Multigres](https://github.com/multigres/multigres) Operator** is a Kubernetes operator for managing distributed, sharded PostgreSQL clusters across multiple failure domains (zones or regions). It provides a unified API to define the topology of your database system, handling the complex orchestration of `shards`, `cells` (failure domains), and `gateways`.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [How it Works](#how-it-works)
- [Configuration & Defaults](#configuration--defaults)
- [Backup & Restore](#backup--restore)
- [Observability](#observability)
- [Webhook & Certificate Management](#webhook--certificate-management)
- [Operator Flags](#operator-flags)
- [Pool Replication & Quorum](#pool-replication--quorum)
- [Constraints & Limits](#constraints--limits-v1alpha1)

## Features
- **Global Cluster Management**: Single source of truth (`MultigresCluster`) for the entire database topology.
- **Automated Sharding**: Manages `TableGroups` and `Shards` as first-class citizens.
- **Direct Pod Management**: Manages individual Pods and PVCs directly (no StatefulSets), enabling targeted decommissioning, rolling updates with primary awareness, and granular PVC lifecycle control.
- **Failover & High Availability**: Orchestrates Primary/Standby failovers across defined Cells.
- **Template System**: Define configuration once (`CoreTemplate`, `CellTemplate`, `ShardTemplate`) and reuse it across the cluster.
- **Hierarchical Defaults**: Smart override logic allowing for global defaults, namespace defaults, and granular overrides.
- **Integrated Cert Management**: Built-in self-signed certificate generation and rotation for validating webhooks, with optional support for `cert-manager`.

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
                ├── 🧠 MultiOrch Resources (Deployment)
                └── 🏊 Pools (Operator-managed Pods + PVCs)

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
> This mechanism may change in future versions. See [Template Propagation](docs/development/template-propagation.md) for details on planned improvements.

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
- Scaling down will **not** delete the PVCs from removed pods

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

⚠️ **Replica Scale-Down Behavior**: Setting `whenScaled: Delete` will **immediately delete PVCs** when the operator removes pods during scale-down. If you scale the replica count back up, new pods will start with **empty volumes** and will need to restore from backup. This is useful for:
- Reducing storage costs in non-production environments
- Stateless-like workloads where data is ephemeral

**Note**: This does NOT affect storage size. Changing PVC storage capacity is handled separately by the operator's **PVC Volume Expansion** feature (see below).

✅ **Production Recommendation**: For production clusters, use the default `Retain/Retain` policy and implement proper backup/restore procedures.

### 5. PVC Volume Expansion

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


---

## Backup & Restore

The operator integrates **pgBackRest** for automated backups, WAL archiving, and point-in-time recovery (PITR). Two storage backends are supported: **S3** (recommended for production and multi-cell clusters) and **Filesystem** (PVC-based, for development/single-node). Backup configuration is fully declarative and propagates from the cluster level down to individual shards.

Key features:
- **Replica-based backups** — backups run on a replica to avoid impacting the primary
- **S3 credential options** — IRSA, static credentials, or EC2 instance metadata
- **Auto-generated TLS** — pgBackRest inter-node TLS is managed automatically, with optional cert-manager support

> [!WARNING]
> Filesystem backups are **cell-local**. Cross-cell failover cannot restore from another cell's backup. Use S3 for multi-cell clusters.

📖 **Full documentation:** [Backup & Restore Guide](docs/backup-restore.md)

## Observability

The operator ships with built-in support for **metrics**, **alerting**, **distributed tracing**, and **structured logging**.

- **Metrics** — Prometheus endpoint with 8 operator-specific metrics + controller-runtime framework metrics
- **Alerts** — 7 pre-configured PrometheusRule alerts with dedicated runbooks ([view runbooks](docs/monitoring/runbooks/))
- **Grafana Dashboards** — Operator health dashboard + per-cluster topology dashboard
- **Distributed Tracing** — OpenTelemetry OTLP support, disabled by default, zero overhead when off
- **Structured Logging** — JSON logging with automatic `trace_id`/`span_id` injection for log-trace correlation

📖 **Full documentation:** [Observability Guide](docs/observability.md) · [Observability Demo](demo/observability/demo.md)

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

1.  Creates a `Certificate` and `ClusterIssuer` resource for cert-manager to manage.
2.  Mounts the cert-manager-provisioned secret to `/var/run/secrets/webhook` so certificates exist on disk at startup.

The operator **automatically detects** the certificate management strategy on startup:
- If certificates already exist on disk and the operator did not previously manage them (no cert-strategy annotation), it assumes an external provider (e.g. cert-manager) and skips internal rotation.
- If no certificates exist on disk, or the operator previously annotated the ValidatingWebhookConfiguration, internal certificate rotation is enabled.

---

## Operator Flags

You can customize the operator's behavior by passing flags to the binary (or editing the Deployment args).

**Required Environment Variables:**

The operator requires two environment variables to be set on the Deployment (these are pre-configured in the install manifests):

| Variable | Description |
| :--- | :--- |
| `POD_NAMESPACE` | Namespace where the operator is deployed. Used for leader election, cert secrets, and cache filtering. |
| `POD_SERVICE_ACCOUNT` | Service account name of the operator pod. Used for webhook configuration. |

**Flags:**

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--webhook-enable` | `true` | Enable the admission webhook server. |
| `--webhook-port` | `9443` | The port that the webhook server serves at. |
| `--webhook-cert-dir` | `/var/run/secrets/webhook` | Directory to read/write webhook certificates. |
| `--webhook-service-name` | `multigres-operator-webhook-service` | Name of the Service pointing to the webhook. |
| `--webhook-service-namespace`| `$POD_NAMESPACE` | Namespace of the webhook service. |
| `--webhook-service-account` | `$POD_SERVICE_ACCOUNT` | Service Account name of the operator. |
| `--metrics-bind-address` | `:8443` | Address for the metrics endpoint. Set to `0` to disable. |
| `--metrics-secure` | `true` | Serve metrics over HTTPS with authentication and authorization. |
| `--enable-http2` | `false` | Enable HTTP/2 for metrics and webhook servers. |
| `--leader-elect` | `false` | Enable leader election (recommended for HA deployments). |

---

## Pool Replication & Quorum

Multigres uses the `ANY_2` durability policy by default, which requires every write to be acknowledged by at least 2 nodes (the primary + 1 synchronous standby). This has implications for how many replicas you should run per cell in `readWrite` pools.

| Replicas per Cell | Configuration | Rolling Upgrade Behavior |
| :--- | :--- | :--- |
| **1** | 1 pod (primary only, no standbys) | **Downtime during upgrades.** No standby to maintain quorum. |
| **2** | 1 primary + 1 standby | **Downtime during upgrades.** Draining the standby leaves zero synchronous standbys, violating `ANY_2`. Upstream multigres rejects the `UpdateSynchronousStandbyList REMOVE` because it would empty the synchronous standby list. |
| **3** (recommended) | 1 primary + 2 standbys | **Zero-downtime upgrades.** One standby can be drained while the other maintains quorum. |

The operator enforces a **hard minimum of 1** replica per cell (the CRD rejects `replicasPerCell: 0`). For `readWrite` pools with fewer than 3 replicas, the webhook returns an **admission warning** (not a rejection) explaining the quorum limitation.

`readOnly` pools are not subject to this warning since they don't participate in write quorum.

---

## Constraints & Limits (v1alpha1)

Please be aware of the following constraints in the current version:

*   **Database Limit**: Only **1** database is supported per cluster. It must be named `postgres` and marked `default: true`.
*   **Shard Naming**: Shards currently must be named `0-inf` - this is a limitation of the current implementation of Multigres.
*   **Naming Lengths**:
    *   **TableGroup Names**: If the combined name (`cluster-db-tg`) exceeds **28 characters**, the operator automatically hashes the database and tablegroup names to ensure that the resulting child resource names (Shards, Pods, PVCs) stay within Kubernetes limits (63 chars).
    *   **Cluster Name**: Recommended to be under **20 characters** to ensure that even with hashing, suffixes fit comfortably.
*   **Immutable Fields**: Some fields like `zone` and `region` in Cell definitions are immutable after creation.
*   **Append-Only Pools and Cells**: Pools and cells cannot be renamed or removed from a cluster. This prevents orphaned pods and stale etcd registrations.
