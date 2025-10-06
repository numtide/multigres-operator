# Interface Design

For a Kubernetes operator, the "interface" is the Custom Resource Definition (CRD) API that users interact with.

## CRD API Design Principles

### Field Naming
- **Clarity Over Brevity**: Use `replicas` not `reps`, `image` not `img`
- **Consistent Terminology**: Match Kubernetes conventions (e.g., `resources` for ResourceRequirements)
- **Avoid Abbreviations**: Except universally understood ones (HPA, CPU, HTTP)
- **Nested Grouping**: Group related fields (e.g., `gateway.image`, `gateway.replicas`)

### Structure and Organization
- **Component Grouping**: Top-level fields for each Multigres component (Gateway, Orch, Pooler, Etcd)
- **Flat Where Possible**: Avoid deep nesting unless it improves clarity
- **Consistent Patterns**: Same structure across components (all have `image`, `replicas`, `resources`)

### Optional vs Required
- **Everything Optional**: All fields should have sensible defaults
- **Validate in Code**: Use kubebuilder markers for basic validation, webhooks for complex rules
- **Document Defaults**: Clearly document default values in API godoc comments

## Status Design

**Purpose**: Status is for human observability, not programmatic consumption.

- **Human-Readable**: Designed for `kubectl get`, `kubectl describe`, dashboard UIs
- **Not for Automation**: External systems should not depend on status fields for decisions
- **Observability Only**: Reflects what operator observes, helps with debugging and monitoring

**For Programmatic Use Instead**:
- Query underlying resources directly (Deployments, StatefulSets, Services)
- Use Kubernetes label selectors to find operator-managed resources
- Watch pod readiness probes and conditions
- Monitor Services and Endpoints for network availability

### Status Structure
- **Per-Component Status**: Separate status for each component (Etcd, Gateway, Orch, Pooler)
- **Ready Flags**: Boolean `ready` field - summary for human review
- **Replica Counts**: Both `replicas` and `readyReplicas` - quick health check
- **Conditions**: Standard Kubernetes conditions for detailed state
- **ObservedGeneration**: Track which spec version was reconciled

### Condition Pattern
- **Type**: `Ready`, `Available`, `Progressing`, `Degraded`
- **Status**: `True`, `False`, `Unknown`
- **Reason**: CamelCase (e.g., `EtcdNotReady`, `DeploymentFailed`)
- **Message**: Human-readable details for debugging

### Status Update Strategy
- **Aggregate First**: Collect all component statuses before updating
- **Single Update**: One status update per reconciliation loop
- **Idempotent**: Safe to recalculate and update repeatedly

## kubectl Output Design

Users interact with the operator primarily through `kubectl`.

### Printer Columns
Define useful columns for `kubectl get multigres`:

```go
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Etcd",type=string,JSONPath=`.status.etcd.replicas`
// +kubebuilder:printcolumn:name="Gateway",type=string,JSONPath=`.status.gateway.replicas`
// +kubebuilder:printcolumn:name="Pooler",type=string,JSONPath=`.status.pooler.replicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
```

**Principles**:
- **Most Important First**: Ready status, then component health
- **Scannable**: Users should quickly see if cluster is healthy
- **Compact**: Fit on standard terminal width (~120 chars)
- **Age Last**: Standard Kubernetes convention

### Resource Naming
- **CR Name**: User-chosen, becomes prefix for all resources
- **Component Resources**: `{name}-{component}` (e.g., `my-db-gateway`, `my-db-etcd`)
- **Predictable**: Users can guess resource names for debugging

## Validation and Error Messages

### CRD Validation Messages
When validation fails, users see the error via `kubectl apply`:

```bash
error: error validating "multigres.yaml": error validating data:
  ValidationError(Multigres.spec.gateway.replicas): invalid type for io.multigres.v1alpha1.Multigres.spec.gateway.replicas:
  got "number", expected "string"; if you choose to ignore these errors, turn validation off with --validate=false
```

**Design Validation for Clarity**:
- Use kubebuilder markers to catch errors at API level
- Provide clear field names that match YAML structure
- Write descriptive validation messages in webhook if used

### Kubernetes Events
Emit events for significant state changes:

```go
r.Recorder.Event(instance, corev1.EventTypeNormal, "Created", "Created etcd StatefulSet")
r.Recorder.Event(instance, corev1.EventTypeWarning, "Failed", "Failed to create gateway Deployment: insufficient permissions")
```

**Event Guidelines**:
- **Type**: Normal for progress, Warning for problems
- **Reason**: CamelCase, concise (e.g., `Created`, `Failed`, `Scaled`)
- **Message**: Specific details - what resource, what action, what error

### Log Messages
Logs appear in operator pod and can be viewed with `kubectl logs`:

- **Info Level**: Normal reconciliation progress
- **Error Level**: Failures that trigger requeue
- **Include Context**: Namespace, name, component being reconciled

## Labels and Annotations

### Standard Labels
Apply consistent labels to all managed resources:

```yaml
labels:
  app.kubernetes.io/name: multigres
  app.kubernetes.io/instance: <cr-name>
  app.kubernetes.io/component: <gateway|orch|pooler|etcd>
  app.kubernetes.io/managed-by: multigres-operator
```

**Purpose**:
- **Selection**: Find resources with label selectors
- **Organization**: Group related resources in dashboards
- **Ownership**: Clear which operator manages the resource

### Annotations
Use annotations for metadata that doesn't affect selection:

```yaml
annotations:
  multigres.com/last-reconciled: "2025-10-04T12:00:00Z"
  multigres.com/version: "v1alpha1"
```

**Guidelines**:
- **Namespace Annotations**: Use `multigres.com/` prefix
- **Standard Kubernetes**: Respect well-known annotations (e.g., `kubectl.kubernetes.io/last-applied-configuration`)
- **Documentation**: Don't rely on annotations for critical logic

## API Documentation

### Godoc Comments
Every field in API types needs documentation:

```go
// Gateway specifies the configuration for MultiGateway deployment.
// If not specified, MultiGateway will not be deployed.
// +optional
Gateway GatewaySpec `json:"gateway,omitempty"`
```

### Example Manifests
Provide examples in `config/samples/`:

- `multigres_v1alpha1_multigres.yaml` - Basic example
- `multigres_minimal.yaml` - Minimal config with all defaults
- `multigres_production.yaml` - Production-ready configuration