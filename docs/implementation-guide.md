# Implementation Guide

<!-- TODO: Customize this guide for your project's specific technology stack and practices -->

## Development Environment

### Language Configuration
- **Language**: Go 1.24+
- **Build System**: Make for task orchestration, Go modules for dependencies
- **Project Structure**: Standard Kubebuilder layout - `api/`, `cmd/`, `internal/`, `config/`
- **Linting**: golangci-lint (configured in `.golangci.yml`)

### Required Tools
- **go**: 1.24 or higher
- **kubectl**: Kubernetes CLI for cluster interaction
- **kind**: Local Kubernetes cluster for testing (or other local cluster)
- **kubebuilder**: Optional - for regenerating CRDs and scaffolding
- **docker**: For building container images

### Optional Tools
- **Nix + direnv**: Reproducible development environment (see `flake.nix` and `.envrc`)
- **kustomize**: Included with kubectl 1.14+, used for manifest management

### Dependency Management
- **Go Modules**: `go.mod` and `go.sum` for dependency tracking
- **Version Pinning**: Pin controller-runtime and client-go to compatible versions
- **Vendor Directory**: Not used - rely on module cache
- **Updating Dependencies**: Use `go get -u` carefully, test thoroughly after updates

## Coding Standards

### Project-Specific Conventions
- **Package Names**: Prefer single-word names (`controller`, `resources`) over multi-word (`resource_builder`)
- **Reconciler Methods**: Name component reconcilers consistently (e.g., `Reconcile(ctx, instance)`)
- **Resource Builders**: Prefix with `Build` (e.g., `BuildEtcdStatefulSet`, `BuildGatewayDeployment`)
- **No Panic in Reconcilers**: Controllers must return errors, never `panic()` - controller-runtime handles retries

### Kubebuilder Markers
Markers go in comments above the target:

```go
// +kubebuilder:rbac:groups=multigres.io,resources=multigres,verbs=get;list;watch
func (r *MultigresReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
```

Common markers:
- **RBAC**: `+kubebuilder:rbac:groups=...,resources=...,verbs=...`
- **Validation**: `+kubebuilder:validation:Minimum=1`, `+kubebuilder:validation:Enum=...`
- **Defaults**: `+kubebuilder:default="value"`
- **Print Columns**: `+kubebuilder:printcolumn:name=...,type=...,JSONPath=...`

### Controller Patterns
- **Context First**: Always pass `context.Context` as first parameter
- **Logger from Context**: Use `log := log.FromContext(ctx)` not global loggers
- **Requeue**: Use `ctrl.Result{Requeue: true}` or `ctrl.Result{RequeueAfter: duration}`
- **Owner References**: Set with `ctrl.SetControllerReference(owner, resource, scheme)` for automatic cleanup

### OpenTelemetry Integration
- **Span Creation**: Wrap reconciliation sections with `tracer.Start(ctx, "span-name")`
- **Metric Recording**: Use OTel meter for custom metrics
- **Log Correlation**: Structured logs automatically include trace context

## Development Practices

### Testing Strategy

**Unit Tests** (internal/resources, internal/webhook):
- Test pure resource builder functions with table-driven tests
- Mock nothing - builders are pure functions
- Focus on correct Kubernetes manifest generation

**Integration Tests** (internal/controller):
- Use `envtest` - provides real Kubernetes API without full cluster
- Test reconciliation loops end-to-end
- Verify resource creation, updates, and status updates

**Test Commands**:
```bash
# Unit tests only (fast)
make test

# Integration tests with envtest
make test-integration

# All tests with coverage
make test-all

# Coverage report
make test-coverage
```

**Test Requirements**:
- **100% Test Coverage Goal**: Aim for 100% coverage for all code unless absolutely impossible
- New resource builders need table-driven unit tests
- New reconciliation logic needs integration tests
- Test both success and error paths
- Test edge cases, error conditions, and boundary values
- Use `testutil` package for common test helpers
- If coverage < 100%, document why in code comments

### Local Development Workflow

```bash
# 1. Create local kind cluster
make kind-cluster

# 2. Install CRDs
make install

# 3. Run operator locally (outside cluster)
make run

# 4. Apply sample CR
kubectl apply -f config/samples/

# 5. Watch logs and test changes
# (operator runs in terminal, ctrl+c to stop)

# 6. Cleanup
make kind-delete
```

### Deployment Testing

```bash
# Build and deploy to kind
make docker-build
make deploy-kind

# Check operator status
make kind-status

# View logs
make kind-logs
```

### Making Changes

**API Changes** (`api/v1alpha1/`):
```bash
# After modifying types, regenerate CRD manifests
make manifests

# Reinstall CRDs
make install
```

**Controller Changes** (`internal/controller/`):
- Run `make test` frequently during development
- Integration tests catch reconciliation bugs early
- Use `make run` for quick iteration (no docker build needed)

**Resource Builder Changes** (`internal/resources/`):
- Write/update table-driven tests first
- Run `make test` - very fast feedback loop
- Integration tests verify end-to-end behavior