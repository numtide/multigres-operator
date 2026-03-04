# Contributing to Multigres Operator

## Development Environment

### Prerequisites

- Go 1.25+
- Docker (for Kind clusters)
- `kubectl`
- `make`

### Setup

```bash
# Clone the repository
git clone https://github.com/numtide/multigres-operator.git
cd multigres-operator

# Run unit tests
make test

# Run integration tests (requires envtest binaries, installed automatically)
make test-integration
```

### Local Development with Kind

```bash
# Create a Kind cluster and deploy the operator
make kind-deploy

# Apply a sample cluster
kubectl apply -f config/samples/minimal.yaml

# Watch resources
kubectl get multigrescluster,cell,shard,tablegroup,toposerver -A -w

# Clean up
make kind-down
```

See the [README](README.md#local-development-kind) for additional Kind deployment options (cert-manager, observability stack).

## Code Style

This project follows the [Google Go Style Guide](https://google.github.io/styleguide/go/best-practices).

Key conventions:
- **No transient comments.** Comments should be helpful permanently, not just during development.
- **Error handling.** Always wrap errors with context using `fmt.Errorf("operation: %w", err)`.
- **Naming.** Use idiomatic Go names. Controllers live in `pkg/{handler}/controller/{resource}/`.
- **Testing.** Write table-driven tests. Use `testify/assert` and `testify/require` for assertions.

## Project Structure

```
api/v1alpha1/       # CRD type definitions (kubebuilder markers)
cmd/                # Operator entrypoint (main.go)
config/             # Kustomize manifests, CRDs, RBAC, samples, monitoring
docs/               # User-facing docs, runbooks, developer docs
  development/      # Internal architecture and implementation references
  monitoring/       # Alert runbooks
pkg/
  cluster-handler/  # MultigresCluster controller (top-level reconciler)
  resource-handler/ # Cell, Shard, TableGroup, TopoServer controllers
  resolver/         # Configuration resolution (4-level override chain)
  webhook/          # Admission webhook (mutating + validating)
  cert/             # Generic TLS certificate lifecycle manager
  monitoring/       # Metrics, tracing, log-trace correlation
  util/             # Shared utilities (naming, status, PVC helpers)
```

## Making Changes

### Modifying CRDs

1. Edit the type definitions in `api/v1alpha1/`
2. Run `make generate manifests` to regenerate CRDs and DeepCopy methods
3. Update the resolver if the new field needs defaulting or validation
4. Update the webhook handlers if admission logic changes
5. Run `make test test-integration` to verify

### Adding a New Controller

1. Create the controller package under `pkg/resource-handler/controller/{resource}/`
2. Implement the `Reconcile` method following existing patterns (see `cell_controller.go` as an example)
3. Register the controller in `cmd/main.go`
4. Add RBAC markers for any new resource types
5. Write unit and integration tests

### Running Tests

| Command | Scope |
|:---|:---|
| `make test` | Unit tests across all modules |
| `make test-integration` | Integration tests using envtest |
| `make test-e2e` | End-to-end tests using Kind clusters |
| `make lint` | Linting via golangci-lint |

## Documentation

- **User-facing docs** go in the [README](README.md) or `docs/` (backup-restore, observability, runbooks)
- **Developer docs** go in `docs/development/`
- **Design proposals** go in `plans/` (mark completed proposals with `## Status: Completed`)

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
feat(shard): add PVC volume expansion support
fix(drain): prevent fallthrough on topo unavailability
docs(readme): update backup architecture section
chore(deps): update internal Go module dependencies
```
