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
