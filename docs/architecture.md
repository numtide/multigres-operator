# System Architecture

## Core Philosophy

### Idiomatic Go
- **Simplicity and Clarity**: Straightforward code over clever abstractions
- **Explicit Dependencies**: Function signatures and struct fields make dependencies obvious
- **Error Handling**: Errors include context and are wrapped for debugging
- **Pure Resource Builders**: Resource functions are deterministic - same input produces same output
- **Structured Concurrency**: Use sync.WaitGroup for coordinated goroutines with clear lifecycle

### Kubernetes Operator Safety
- **Finalizers for Lifecycle**: Prevent resource deletion until cleanup is complete
- **Owner References for Cleanup**: Automatic garbage collection of child resources when parent is deleted
- **Idempotent Reconciliation**: Safe to run reconcile loop multiple times - converges to desired state
- **Status Subresource**: Observed state lives in status, never in spec
- **Infrastructure-Only Concerns**: Operator manages compute resources, not application logic or startup dependencies

## Design Principles

### Clean Separation of Concerns
- **Operator**: Manages Kubernetes resources (Deployments, StatefulSets, Services, HPAs)
- **Multigres Components**: Handle their own application logic, readiness, and inter-service dependencies
- **No Application Knowledge**: Operator doesn't orchestrate Multigres component startup order
- **Eventually Consistent**: All resources created concurrently; components become ready when dependencies are available

### Layered Architecture
- **Controller Layer**: Orchestrates reconciliation, manages finalizers and status updates
- **Reconciler Layer**: Component-specific reconciliation (etcd, multigateway, multiorch, multipooler)
- **Resource Builder Layer**: Pure functions that construct Kubernetes manifests
- **Parallel Reconciliation**: All component reconcilers run concurrently via goroutines

### Testability
- **Pure Functions**: Resource builders are deterministic and table-test friendly
- **Test Helpers**: Mocks and test doubles enable testing without external dependencies
- **Integration Tests**: envtest provides real Kubernetes API for controller testing
- **Minimal Interfaces**: Each reconciler has consistent, simple signature

## System Architecture

```
multigres-operator/
├── go.mod                  # Root module (primarily for cmd)
├── cmd/multigres-operator/ # Main entry point
├── pkg/
│   ├── cluster-handler/    # Cluster-level orchestration (separate Go module)
│   │   ├── go.mod
│   │   └── controller/
│   │       └── multigrescluster/  # MultigresCluster reconciler
│   ├── data-handler/       # Data plane management (separate Go module)
│   │   ├── go.mod
│   │   └── controller/
│   │       └── cell/       # Cell reconciler
│   └── resource-handler/   # Component resources (separate Go module)
│       ├── go.mod
│       └── controller/
│           ├── multigateway/
│           ├── multiorch/
│           ├── multipooler/
│           └── etcd/
├── config/                 # Kubernetes manifests for operator deployment
├── docs/                   # Architecture, conventions, and development guides
└── plans/                  # Planning documents

Note: go.work file can be created locally for development convenience. It is in .gitignore.
```

### Multi-Module Architecture

The operator is organized into three independent Go modules with clear separation of concerns. Developers can use **Go workspaces** locally (`go.work`) for easier cross-module development, though this file is not committed to the repository:

#### Cluster Handler Module (`pkg/cluster-handler`)
- **Purpose**: High-level cluster orchestration
- **Responsibilities**:
  - Manages MultigresCluster custom resource
  - Coordinates creation of child resources (MultiGateway, MultiOrch, MultiPooler, Etcd, Cell)
  - Aggregates status from child resources
  - Handles cluster-wide lifecycle (finalizers, upgrades)
- **Independence**: Can be tested and versioned separately from resource handlers

#### Data Handler Module (`pkg/data-handler`)
- **Purpose**: Data plane management and topology
- **Responsibilities**:
  - Manages Cell custom resource (data topology abstraction)
  - Handles data plane configuration and routing
  - Coordinates with Vitess/Multigres data components
- **Independence**: Data plane logic isolated from control plane and resource management

#### Resource Handler Module (`pkg/resource-handler`)
- **Purpose**: Individual component resource management
- **Responsibilities**:
  - Manages component CRDs: MultiGateway, MultiOrch, MultiPooler, Etcd
  - Creates and reconciles Kubernetes resources (Deployments, StatefulSets, Services, HPAs)
  - Implements pure resource builder functions
  - Component-specific health checking and status reporting
- **Independence**: Component reconcilers can evolve independently of cluster orchestration

### Benefits of Multi-Module Structure

- **Clear Boundaries**: Each module has distinct responsibilities and can be understood independently
- **Independent Testing**: Unit and integration tests scoped to each module
- **Parallel Development**: Teams can work on different modules without conflicts
- **Selective Imports**: Main binary only imports what it needs from each module
- **Version Flexibility**: Modules can evolve at different paces (though currently developed in lockstep)

## Core Components

### Reconciliation Flow

The operator follows a standard Kubernetes reconciliation pattern:

1. **Watch**: Monitor Multigres custom resources for changes
2. **Reconcile**: When changes detected, run reconciliation loop
3. **Converge**: Create/update Kubernetes resources to match desired state
4. **Status Update**: Reflect observed state in Multigres status subresource
5. **Requeue**: Schedule next reconciliation if needed

### Component Reconcilers

The **resource-handler** module contains individual reconcilers for each Multigres component:

- **etcd Reconciler** (`pkg/resource-handler/controller/etcd`): Manages StatefulSet for etcd cluster, headless and client Services
- **multigateway Reconciler** (`pkg/resource-handler/controller/multigateway`): Manages Deployment, Service, and optional HPA for MultiGateway
- **multiorch Reconciler** (`pkg/resource-handler/controller/multiorch`): Manages Deployment and optional HPA for MultiOrch
- **multipooler Reconciler** (`pkg/resource-handler/controller/multipooler`): Manages StatefulSet with multi-container pods (pooler, pgctld, postgres), and optional HPA

All component reconcilers run in parallel and are independent of each other.

### Resource Builders

Pure functions that generate Kubernetes manifests:

- **Deterministic**: Same inputs always produce same outputs
- **No Side Effects**: Don't make API calls or modify global state
- **Testable**: Easily unit tested in isolation
- **Composable**: Small functions that build specific resource types

### Validation Strategy

Using **CRD validation markers only** for simplicity:

- OpenAPI v3 schema constraints (numeric ranges, enums, patterns, etc.)
- Default values specified in CRD
- API server enforces validation without external calls
- No admission webhooks required

### Observability

**OpenTelemetry Integration**:
- **Traces**: Reconciliation flow, API calls, component creation with spans
- **Metrics**: Reconciliation duration, error rates, component health, resource counts
- **Logs**: Structured logs with trace context correlation

**Health Checks**:
- **Liveness Probe**: HTTP endpoint to detect if operator needs restart
- **Readiness Probe**: HTTP endpoint to indicate operator can handle requests

**Kubernetes Events**:
- Emitted for significant state changes and errors
- Surfaced via `kubectl describe` for user visibility

### Admission Webhooks (Future Consideration)

**Current Decision**: Start without admission webhooks to keep installation simple and reduce moving parts during initial development.

**When to Add**: Consider webhooks when:
- Need validation beyond OpenAPI v3 schema (cross-field validation, complex business rules)
- Want dynamic defaults based on cluster state
- Need mutation beyond simple defaults

**Certificate Management Options**:
- **Init container pattern** (preferred): Jobs generate certs, init container waits - no runtime dependencies, standard CNCF pattern
- **cert-manager**: Automatic cert management - adds runtime dependency but simplifies renewal
- **Manual certificates**: Full control - operational overhead for rotation

Current preference is init container pattern (same as Istio, NGINX Ingress), but final decision will be made during implementation based on operational requirements.

## Technology Stack

### Language and Runtime
- **Language**: Go 1.25+
- **Key Features**: Concurrency, interfaces, strong typing, workspaces for multi-module projects

### Key Dependencies
- **Framework**: Kubebuilder v3 - scaffolding and patterns for Kubernetes operators
- **controller-runtime**: Core controller and client libraries
- **client-go**: Kubernetes API client
- **OpenTelemetry**: Traces, metrics, and logs
- **Testing**: Standard Go testing, envtest for integration tests

### Build Tools
- **Make**: Task orchestration (build, test, deploy)
- **Docker**: Container image building
- **kubectl/kustomize**: Kubernetes manifest management
- **GitHub Actions**: CI/CD pipeline for testing and releases
- **Optional**: Nix + direnv for reproducible dev environment

## Performance Considerations

### Parallel Reconciliation
- Component reconcilers run concurrently
- Each component reconciler is independent and stateless
- All components complete before status aggregation

### Resource Efficiency
- Pure resource builders don't allocate unnecessary memory
- Status updates batched - one update per reconciliation loop
- Requeue delays prevent tight loops when resources aren't ready

### Kubernetes API Calls
- Owner references enable automatic garbage collection (no manual cleanup)
- Watches reduce unnecessary reconciliation triggers
- Client-side caching via controller-runtime reduces API server load

## Error Handling Strategy

### Error Types
- **Reconciliation Errors**: Failed to create/update Kubernetes resources
- **API Errors**: Kubernetes API server communication failures
- **Validation Errors**: Invalid spec values caught by CRD validation
- **Health Check Errors**: Component not ready yet (non-fatal, triggers requeue)

### Error Reporting
- Errors wrapped with context for debugging
- Structured logging with key-value pairs
- Critical errors returned to trigger requeue
- Non-critical errors logged but don't fail reconciliation

### Recovery Strategies
- **Automatic Requeue**: Failed reconciliations automatically retry with exponential backoff
- **Status Conditions**: Error details reflected in status conditions for debugging
- **Idempotent Operations**: Safe to retry - won't duplicate resources
- **Component Independence**: One component's failure doesn't block others

## Future Architecture Considerations

### High Availability
- Leader election (already supported via controller-runtime flag)
- Multiple operator replicas for redundancy
- Graceful shutdown handling for in-flight reconciliations
- Zero-downtime upgrades

### Advanced Features
- Custom resource pruning and cleanup policies
- Multi-cluster support for Multigres deployments
- Backup and restore integration
- Advanced scheduling and placement strategies
