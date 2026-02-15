---
title: Integrate validating webhook for cluster state aware validation
state: draft
tags: [validation, webhook, security, data-handler]
---

# Summary

Add a Kubernetes validating admission webhook to enhance the current CRD-based validation with cluster-state-aware validation rules for Multigres custom resources. The webhook will complement existing CRD validation by providing cross-field validation, cluster topology validation, and runtime constraint checking that cannot be expressed through OpenAPI schema alone.

# Motivation

Currently, the operator relies on CRD validation markers for field-level validation (type checks, numeric ranges, enums). While this provides a solid foundation, it cannot enforce complex business rules that depend on:

- **Cross-field validation**: Ensuring consistency between related fields (e.g., storage size must increase on updates, replica count changes must respect quorum requirements)
- **Cluster topology awareness**: Validating that Cell configurations are consistent with the topology (e.g., preventing deletion of cells that still have active data)
- **Runtime state checks**: Ensuring updates don't violate current cluster state (e.g., can't reduce etcd replicas below current ready count without explicit force flag)
- **Multi-resource validation**: Checking relationships between MultigresCluster, Cell, and component resources

Without a validating webhook, users can create invalid configurations that will fail during reconciliation, leading to:
- Confusing error messages in operator logs instead of immediate kubectl feedback
- Partially applied changes that leave clusters in inconsistent states
- Difficulty debugging misconfigurations

## Goals

- Implement validating webhook for MultigresCluster with cluster-state-aware validation
- Provide clear, actionable error messages at admission time (before reconciliation)
- Support multiple deployment strategies: with and without cert-manager
- Maintain testability with envtest for integration tests
- Keep webhook logic in data-handler module (cluster topology concerns)
- Document webhook configuration and certificate management approaches
- Keep webhook optional initially - operator should function (with degraded UX) if webhook is unavailable

## Non-Goals

- **Mutating webhooks**: Not needed initially, but architecture should allow future addition
- **Validation for all CRDs**: Start with MultigresCluster; extend to Cell if needed later
- **Complex certificate rotation**: Use industry-standard patterns (cert-manager or init containers)
- **Replacing CRD validation**: Webhook complements CRD validation, doesn't replace it

# Proposal

## Architecture

```
pkg/data-handler/
├── go.mod
├── webhook/
│   ├── multigrescluster_webhook.go       # Webhook implementation
│   ├── multigrescluster_webhook_test.go  # Unit tests
│   └── validation.go                     # Validation logic (pure functions)
└── controller/
    └── multigrescluster/
        └── reconciler.go
```

Webhook lives in **data-handler module** because MultigresCluster validation requires understanding of cluster topology and cell relationships, which is the primary concern of data-handler.

## Validation Rules

### Create Validation

When creating a MultigresCluster:

1. **Cell topology validation**:
   - At least one cell must be defined in spec.cells
   - Cell names must be unique
   - Each cell must have valid zone/region topology

2. **Component configuration validation**:
   - Etcd replica count must be odd (1, 3, 5) for quorum
   - Resource requests must not exceed limits
   - Image references must be valid (basic format check)

3. **Storage validation**:
   - StorageSize must be parseable as Kubernetes quantity
   - If VolumeClaimTemplate is specified, it must be valid

### Update Validation

When updating a MultigresCluster:

1. **Immutable field enforcement**:
   - Cannot change cell names (would break data topology)
   - Cannot reduce storage size (Kubernetes PVC limitation)

2. **Safe replica scaling**:
   - Etcd replicas must remain odd
   - Cannot reduce etcd replicas below current ready count without `.spec.forceScale: true` flag

3. **Cell deletion safety**:
   - Cannot remove cells that still have active data (check via Cell CR status)
   - Require `.spec.cells[].drainBeforeDelete: true` for safe cell removal

4. **Cross-field consistency**:
   - If HA mode is enabled, must have etcd replicas >= 3
   - Multi-cell deployments must have proper topology spread configuration

### Delete Validation

Currently no delete validation needed - finalizers handle cleanup. Could add warning if cluster still has active cells.

## Webhook Configuration

The webhook will be registered as a `ValidatingWebhookConfiguration`:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: multigres-validating-webhook
webhooks:
  - name: multigrescluster.multigres.com
    rules:
      - apiGroups: ["multigres.com"]
        apiVersions: ["v1alpha1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["multigresclusters"]
    clientConfig:
      service:
        name: multigres-operator-webhook
        namespace: multigres-system
        path: /validate-multigres-com-v1alpha1-multigrescluster
      caBundle: ${CA_BUNDLE}  # Injected by cert manager or init container
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 10
    failurePolicy: Fail  # Reject requests if webhook unavailable (safety first)
```

# Design Details

## Implementation Components

### 1. Webhook Server

Located in `pkg/data-handler/webhook/multigrescluster_webhook.go`:

```go
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-multigrescluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=multigresclusters,verbs=create;update,versions=v1alpha1,name=multigrescluster.multigres.com,admissionReviewVersions=v1

type MultigresClusterValidator struct {
    Client client.Client
}

func (v *MultigresClusterValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)
func (v *MultigresClusterValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error)
func (v *MultigresClusterValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)
```

### 2. Validation Logic

Pure functions in `pkg/data-handler/webhook/validation.go`:

```go
// ValidateEtcdReplicas ensures etcd replica count is odd
func ValidateEtcdReplicas(replicas int32) error

// ValidateStorageIncrease ensures storage size only increases
func ValidateStorageIncrease(oldSize, newSize string) error

// ValidateCellTopology checks cell configuration consistency
func ValidateCellTopology(cells []CellSpec) error

// ValidateImmutableFields ensures immutable fields haven't changed
func ValidateImmutableFields(old, new *MultigresCluster) error
```

These pure functions are easily unit testable without Kubernetes dependencies.

### 3. Certificate Management

Two deployment strategies supported:

#### Strategy A: cert-manager (Recommended for Production)

**Pros**:
- Automatic certificate generation and renewal
- Well-tested, widely adopted (used by 60%+ of operators)
- Handles CA bundle injection automatically
- No custom init container code to maintain

**Cons**:
- Additional runtime dependency (cert-manager must be installed)
- Slightly more complex initial setup

**Implementation**:

```yaml
# config/webhook/cert-manager/certificate.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: multigres-webhook-cert
  namespace: multigres-system
spec:
  secretName: multigres-webhook-tls
  dnsNames:
    - multigres-operator-webhook.multigres-system.svc
    - multigres-operator-webhook.multigres-system.svc.cluster.local
  issuerRef:
    kind: Issuer
    name: multigres-webhook-issuer
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: multigres-webhook-issuer
  namespace: multigres-system
spec:
  selfSigned: {}
```

Cert-manager's CA injector automatically populates `caBundle` in ValidatingWebhookConfiguration.

#### Strategy B: Init Container (Recommended for Simplicity)

**Pros**:
- No runtime dependencies - pure Kubernetes primitives
- Standard CNCF pattern (Istio, NGINX Ingress use this)
- Simpler for development and testing
- Self-contained operator deployment

**Cons**:
- Manual certificate rotation (can automate with CronJob if needed)
- Slightly longer pod startup time

**Implementation**:

```yaml
# config/webhook/init-container/certificate-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: multigres-webhook-cert-setup
  namespace: multigres-system
spec:
  template:
    spec:
      serviceAccountName: multigres-webhook-cert-generator
      containers:
      - name: cert-generator
        image: registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.3.0
        args:
          - create
          - --host=multigres-operator-webhook.multigres-system.svc
          - --namespace=multigres-system
          - --secret-name=multigres-webhook-tls
          - --cert-name=tls.crt
          - --key-name=tls.key
      restartPolicy: OnFailure
---
# Operator deployment init container
initContainers:
- name: wait-for-webhook-cert
  image: busybox:1.36
  command: ['sh', '-c', 'until [ -f /tmp/certs/tls.crt ]; do echo waiting for webhook cert; sleep 2; done']
  volumeMounts:
  - name: webhook-certs
    mountPath: /tmp/certs
```

CA bundle injection handled by separate Job that patches ValidatingWebhookConfiguration:

```bash
# Extract CA and patch webhook config
CA_BUNDLE=$(kubectl get secret multigres-webhook-tls -n multigres-system -o jsonpath='{.data.ca\.crt}')
kubectl patch validatingwebhookconfiguration multigres-validating-webhook \
  --type='json' -p="[{'op': 'replace', 'path': '/webhooks/0/clientConfig/caBundle', 'value':'${CA_BUNDLE}'}]"
```

### 4. Webhook Server Setup

Modify `cmd/multigres-operator/main.go` to register webhook:

```go
import (
    datahandlerwebhook "github.com/numtide/multigres-operator/pkg/data-handler/webhook"
)

func main() {
    // ... existing setup ...

    // Setup webhook if webhook server is enabled
    if webhookEnabled {
        validator := &datahandlerwebhook.MultigresClusterValidator{
            Client: mgr.GetClient(),
        }

        if err := ctrl.NewWebhookManagedBy(mgr).
            For(&multigresv1alpha1.MultigresCluster{}).
            WithValidator(validator).
            Complete(); err != nil {
            setupLog.Error(err, "unable to create webhook")
            os.Exit(1)
        }
    }
}
```

### 5. Kustomize Overlays

```
config/
├── webhook/
│   ├── cert-manager/          # Cert-manager based deployment
│   │   ├── kustomization.yaml
│   │   ├── certificate.yaml
│   │   └── issuer.yaml
│   ├── init-container/        # Init container based deployment
│   │   ├── kustomization.yaml
│   │   ├── certificate-job.yaml
│   │   └── ca-injection-job.yaml
│   └── manifests/             # Shared webhook resources
│       ├── service.yaml
│       ├── validatingwebhookconfiguration.yaml
│       └── rbac.yaml
└── default/
    └── kustomization.yaml     # Include webhook overlay
```

## Test Plan

### Unit Tests

Test validation logic in isolation (no Kubernetes dependency):

```go
func TestValidateEtcdReplicas(t *testing.T) {
    tests := []struct {
        name    string
        replicas int32
        wantErr bool
    }{
        {"odd replicas valid", 3, false},
        {"even replicas invalid", 2, true},
        {"single replica valid", 1, false},
        {"zero replicas invalid", 0, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateEtcdReplicas(tt.replicas)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateEtcdReplicas() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Integration Tests

Use envtest to test webhook end-to-end:

```go
func TestWebhookValidation(t *testing.T) {
    // Setup envtest with webhook
    testEnv := &envtest.Environment{
        WebhookInstallOptions: envtest.WebhookInstallOptions{
            Paths: []string{filepath.Join("..", "..", "config", "webhook")},
        },
    }

    // Test that invalid MultigresCluster is rejected
    invalidCluster := &multigresv1alpha1.MultigresCluster{
        Spec: multigresv1alpha1.MultigresClusterSpec{
            Etcd: multigresv1alpha1.EtcdSpec{
                Replicas: ptr.To(int32(2)), // Even number - should fail
            },
        },
    }

    err := k8sClient.Create(ctx, invalidCluster)
    require.Error(t, err)
    require.Contains(t, err.Error(), "etcd replica count must be odd")
}
```

### Manual Testing

1. Deploy operator with webhook enabled
2. Attempt to create MultigresCluster with invalid config:
   ```bash
   kubectl apply -f - <<EOF
   apiVersion: multigres.com/v1alpha1
   kind: MultigresCluster
   metadata:
     name: test-cluster
   spec:
     etcd:
       replicas: 2  # Should be rejected
   EOF
   ```
3. Verify clear error message returned immediately
4. Test update validation by attempting unsafe operations

## Graduation Criteria

### MVP (Minimum Viable Product)

- [ ] Webhook implementation for MultigresCluster create/update validation
- [ ] Init-container deployment strategy implemented and documented
- [ ] Basic validation rules implemented (etcd replicas, storage size, immutable fields)
- [ ] Unit tests achieving 100% coverage of validation logic
- [ ] Integration tests with envtest covering major validation paths
- [ ] Documentation for webhook deployment and troubleshooting
- [ ] Webhook is optional (operator functions without it)

### Production Ready

- [ ] Cert-manager deployment strategy implemented and documented
- [ ] Webhook enabled by default in production deployments
- [ ] Advanced validation rules (cell topology, cross-field consistency)
- [ ] Webhook metrics and observability (validation failures, latency)
- [ ] Production testing with both deployment strategies
- [ ] User feedback incorporated into validation messages
- [ ] Performance benchmarks showing <100ms validation latency
- [ ] Certificate rotation tested and documented

### Future Enhancements

- [ ] Webhook high availability with multiple replicas
- [ ] Comprehensive validation covering all edge cases
- [ ] Consider making webhook required for production use

## Upgrade / Downgrade Strategy

### Enabling Webhook

Initial deployment may not have webhook. When adding webhook:

1. Deploy webhook resources (Service, RBAC, certificate Job/Certificate)
2. Wait for certificates to be generated
3. Deploy ValidatingWebhookConfiguration
4. Enable webhook in operator deployment (set `--enable-webhook=true` flag)
5. Rolling restart of operator pods

### Disabling Webhook

If webhook needs to be disabled:

1. Delete ValidatingWebhookConfiguration (allows CRs to be admitted without webhook)
2. Disable webhook in operator deployment (set `--enable-webhook=false` flag)
3. Webhook resources can remain deployed (dormant) or be removed

### Backward Compatibility

- Webhook must accept all CRs that passed CRD validation in previous versions
- New stricter rules only enforced on new CREATE operations
- UPDATE operations can have grace period for tightening rules

## Version Skew Strategy

### Multiple Operator Versions

- ValidatingWebhookConfiguration `failurePolicy: Fail` ensures only one version's webhook is active
- Leader election ensures only one operator replica processes reconciliation
- Webhook logic versioned with API version (v1alpha1 vs v1beta1 webhooks separate)

### Kubernetes Version Compatibility

- Use admission/v1 (Kubernetes 1.16+) instead of admission/v1beta1
- Test with multiple Kubernetes versions in CI (1.25, 1.26, 1.27, 1.28)
- Document minimum Kubernetes version requirement (1.25+)

# Implementation History

- 2025-10-16: Initial draft created

# Drawbacks

1. **Increased Complexity**: Webhooks add moving parts (certs, network, admission chain)
   - *Mitigation*: Provide both simple (init container) and production (cert-manager) paths

2. **Deployment Dependencies**: Webhooks require additional setup vs pure CRD validation
   - *Mitigation*: Make webhook optional initially

3. **Performance Impact**: Every CREATE/UPDATE goes through webhook (adds ~10-50ms latency)
   - *Mitigation*: Keep validation logic simple, add timeout=10s, monitor webhook latency

4. **Certificate Management**: TLS certs can expire, causing admission failures
   - *Mitigation*: Cert-manager auto-renews; init container approach uses long-lived certs with documented rotation

5. **Testing Complexity**: Webhook integration tests require more setup than controller tests
   - *Mitigation*: Pure validation functions are unit testable; envtest handles webhook testing

# Alternatives

## Alternative 1: CRD Validation Only (Current Approach)

**Approach**: Use only kubebuilder validation markers, no webhook.

**Pros**:
- Simplest deployment (no certs, no webhook server)
- No runtime dependencies
- Lowest latency
- Currently working

**Cons**:
- Cannot enforce cross-field validation (etcd replicas vs ready count)
- Cannot validate against cluster state (cell deletion safety)
- Poor error messages for complex validation failures
- Errors only discovered during reconciliation

**Decision**: This is the **current approach** and provides a solid foundation. However, as the operator matures and validation requirements become more complex, we will likely need to enhance this with a validating webhook for cluster-state-aware validation.

## Alternative 2: Client-Side Validation (kubectl plugin)

**Approach**: Provide `kubectl-multigres validate` plugin for pre-flight checks.

**Pros**:
- No server-side complexity
- Can be used in CI/CD pipelines
- User can opt-in to validation

**Cons**:
- Users can bypass validation (kubectl apply still works)
- No protection against API-level access (controller, Terraform, etc.)
- Duplicate validation logic (client and operator)

**Decision**: Could be complementary, but not a replacement for admission webhook.

## Alternative 3: Validating in Reconciler Only

**Approach**: Validate during reconciliation, update status with errors.

**Pros**:
- No webhook complexity
- Validation has full cluster context

**Cons**:
- Errors appear in status, not at admission time (poor UX)
- User sees "success" from kubectl apply, then discovers error later
- Partial reconciliation can leave cluster in inconsistent state

**Decision**: This is part of the current approach for runtime validation, but doesn't replace admission-time validation for better UX.

## Alternative 4: OPA/Gatekeeper Integration

**Approach**: Use Open Policy Agent with Gatekeeper for policy-based validation.

**Pros**:
- Powerful policy engine
- Can validate across multiple resource types
- Policy-as-code approach

**Cons**:
- Heavy dependency (entire OPA stack)
- Requires learning Rego policy language
- Overkill for operator-specific validation
- Users must manage OPA separately

**Decision**: Rejected - too heavyweight for operator-specific validation needs.

# Infrastructure Needed

## Development

- **envtest**: Already available in kubebuilder setup
- **Test certificates**: Generated automatically by envtest
- **No additional infrastructure**: Webhook testing integrated into existing test suite

## Production

### Cert-Manager Strategy

- **cert-manager**: Must be installed in cluster before operator deployment
  - Installation: `kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml`
  - Documented in operator installation guide

### Init Container Strategy

- **Certificate Generator Image**: `registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.3.0`
  - Public image, no private registry needed
  - Job runs once during installation
  - Can be included in airgapped environments by mirroring

### Both Strategies

- **Network**: Webhook service must be reachable from API server
  - Default Kubernetes networking (no special configuration)
  - Service in same namespace as operator

- **RBAC**: Additional permissions for webhook
  - Read access to MultigresCluster CRs (for state validation)
  - Read access to Cell CRs (for topology validation)
  - Already covered by existing operator RBAC

## CI/CD

- **GitHub Actions**: Existing workflow extended to test webhook
  - Add envtest with webhook enabled
  - Test both cert strategies in separate jobs
  - No additional infrastructure needed (runs in GitHub runners)

# Future Enhancements

## Mutating Webhook

Currently not needed, but architecture supports future addition:

**Potential use cases**:
- Auto-populate default values beyond CRD defaults (e.g., intelligent replica counts based on cluster size)
- Inject recommended tolerations/affinity based on cluster topology
- Automatically set recommended resource limits based on observed usage

**Implementation path**:
- Add `MutatingWebhookConfiguration` alongside validating webhook
- Implement `Default()` method in webhook handler
- Separate mutating logic from validation logic (different files)

**Decision**: Defer until clear use case emerges - validation is the immediate need.

## Multi-Resource Validation

Extend webhook to other CRDs:

- **Cell**: Validate cell configuration consistency
- **Component CRs** (Etcd, MultiGateway, etc.): Validate against parent MultigresCluster

**Decision**: Start with MultigresCluster, extend based on user feedback.

## Admission Webhooks for Deletion Protection

Add delete validation for safer cluster teardown:

- Prevent MultigresCluster deletion if cells still have data
- Require confirmation flag for destructive operations
- Block component deletion if still referenced by cluster

**Decision**: Consider after create/update validation is proven.
