---
title: Create Cell management Go module as Multigres aware operator internal
state: draft
tags: []
---

# Summary

Implement Cell management functionality through a `Cell` CRD and reconciler in the `pkg/data-handler` module. A Cell represents a logical grouping of Multigres components and must be registered in the Topo Server (etcd) for the cluster to function. The Cell reconciler automates Cell registration, updates, and cleanup by interacting with the Topo Server using Multigres APIs.

This allows users to manage Cell lifecycle declaratively through Kubernetes, rather than manually configuring entries in etcd.

# Motivation

Multigres clusters require Cell entries in the Topo Server to coordinate components. Without proper Cell registration:
- Components cannot discover each other
- Routing and coordination fail
- The cluster cannot function

Manual Cell management is error-prone and doesn't fit the Kubernetes operator pattern. By implementing a Cell CRD and reconciler, we enable:

1. **Declarative Cell Management**: Users define Cells as Kubernetes resources
2. **Automated Registration**: Operator handles Topo Server interactions
3. **Lifecycle Management**: Cell updates and cleanup happen automatically
4. **Kubernetes-Native**: Cell state visible through `kubectl`, consistent with other operator resources
5. **Integration**: MultigresCluster can create Cells automatically as part of cluster setup

## Goals
- Implement Cell CRD and reconciler for managing Cell entries in Topo Server
- Enable automated Cell registration when Multigres components are deployed
- Handle Cell lifecycle (creation, updates, deletion) through Kubernetes API
- Integrate Cell management into the operator's multi-module architecture

## Non-Goals
- Managing individual Multigres component resources (handled by resource-handler module)
- Orchestrating component startup order (components handle their own dependencies)
- Direct etcd manipulation outside of Cell definitions
- Implementing general-purpose Topo Server management

# Proposal

Implement Cell management in the `pkg/data-handler` module with a `Cell` CRD and reconciler that manages Cell entries in the Topo Server (etcd).

## Cell CRD Structure

```go
type CellSpec struct {
    // Name of the cell
    Name string `json:"name"`

    // Etcd endpoints for Topo Server
    EtcdEndpoints []string `json:"etcdEndpoints"`

    // Optional TLS configuration for etcd
    TLS *TLSConfig `json:"tls,omitempty"`

    // References to Multigres components in this cell
    Components CellComponentsSpec `json:"components"`
}

type CellComponentsSpec struct {
    // MultiGateway references
    Gateways []corev1.ObjectReference `json:"gateways,omitempty"`

    // MultiPooler references
    Poolers []corev1.ObjectReference `json:"poolers,omitempty"`
}

type CellStatus struct {
    // Registered indicates if Cell is registered in Topo Server
    Registered bool `json:"registered"`

    // TopoServerReachable indicates if etcd is accessible
    TopoServerReachable bool `json:"topoServerReachable"`

    // Conditions for Cell state
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

## Reconciliation Logic

The Cell reconciler performs these steps:

1. **Validate etcd connectivity**: Ensure Topo Server endpoints are reachable
2. **Register Cell**: Create or update Cell entry in Topo Server using Multigres APIs
3. **Update component references**: Register component locations in Cell definition
4. **Handle finalizers**: Clean up Cell entry from Topo Server on deletion
5. **Update status**: Reflect registration state and any errors

## Integration with Other Modules

- **MultigresCluster** (in cluster-handler) can create Cell resources as part of cluster setup
- **Component CRDs** (in resource-handler) are referenced by Cell but managed independently
- Cell reconciler uses Multigres libraries to interact with Topo Server

# Design Details

## Module Location

Cell reconciler lives in `pkg/data-handler/controller/cell/`. This module has its own `go.mod` and can include Multigres dependencies.

## Reconciler Implementation

```go
func (r *CellReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch Cell CR
    // 2. Add finalizer if not present
    // 3. Handle deletion (remove from Topo Server)
    // 4. Connect to etcd using provided endpoints
    // 5. Register/update Cell in Topo Server
    // 6. Validate component references exist
    // 7. Update Cell status
    // 8. Requeue if needed
}
```

## Finalizer Handling

- Add finalizer `cell.multigres.io/finalizer` on Cell creation
- On deletion, remove Cell entry from Topo Server before removing finalizer
- Ensures Cell is properly deregistered before CR is deleted

## Error Handling

- **Etcd unreachable**: Set `TopoServerReachable: false`, requeue with backoff
- **Invalid component references**: Log warning, continue (components may not exist yet)
- **Registration failure**: Update Condition with error details, requeue

## Test Plan

1. **Unit Tests**:
   - Test Cell spec validation
   - Test etcd client configuration
   - Mock Topo Server interactions

2. **Integration Tests**:
   - Use envtest with real etcd for Topo Server
   - Test Cell registration and updates
   - Test finalizer cleanup removes Cell from etcd
   - Test reconciliation with missing component references

3. **E2E Tests**:
   - Deploy full Multigres cluster with Cell
   - Verify Cell appears in Topo Server
   - Delete Cell, verify cleanup

## Version Skew Strategy

**Cell CRD vs Multigres Version**:
- Cell reconciler must be compatible with Multigres Topo Server schema
- Pin Multigres dependency to compatible version in `pkg/data-handler/go.mod`
- Document required Multigres version range in CRD

**Topo Server Schema Changes**:
- If Multigres changes Cell schema, update reconciler and CRD together
- Use conversion webhooks if Cell CRD version changes

# Implementation History

- 2025-10-09: Initial draft created

# Drawbacks

**Multigres Dependency**: The data-handler module must depend on Multigres codebase, creating a version coupling. If Multigres Topo Server schema changes, the operator must be updated.

**Etcd Direct Access**: Cell reconciler requires direct etcd access, which may have security implications. Operators must ensure proper network policies and authentication.

**Additional CRD Complexity**: Adds another CRD for users to understand, though Cell is a fundamental Multigres concept.

# Alternatives

## Alternative 1: Manual Cell Registration

Users manually create Cell entries in etcd using Multigres tools.

**Pros**:
- No operator code needed
- Users have complete control

**Cons**:
- Error-prone manual process
- No Kubernetes-native lifecycle management
- Doesn't integrate with MultigresCluster automation
- No cleanup on deletion

**Rejected**: Doesn't fit the operator pattern and increases operational burden.

## Alternative 2: Cell Registration in Component Controllers

Each component reconciler (MultiGateway, MultiPooler) registers itself in the Topo Server.

**Pros**:
- No separate Cell CRD needed
- Decentralized registration

**Cons**:
- Adds Multigres dependency to all component controllers
- No centralized Cell definition
- Coordination between components becomes complex
- Harder to manage Cell lifecycle

**Rejected**: Violates separation of concerns and spreads Multigres dependencies across all modules.

## Alternative 3: Init Job for Cell Registration

Use a Kubernetes Job to register Cell on cluster creation.

**Pros**:
- Simple one-time registration
- No ongoing reconciliation needed

**Cons**:
- No automatic updates if Cell definition changes
- No cleanup on deletion
- Can't handle Cell modifications
- Not declarative - Cell state not in Kubernetes API

**Rejected**: Doesn't provide proper lifecycle management.

# Infrastructure Needed

- **Multigres Codebase**: `pkg/data-handler` module depends on `github.com/multigres/multigres`
- **Etcd Access**: Cell reconciler needs network access to etcd (Topo Server)
- **Kubebuilder**: For generating Cell CRD manifests
- **Testing Etcd**: envtest setup must include etcd for integration tests
