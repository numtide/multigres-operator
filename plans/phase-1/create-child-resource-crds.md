---
title: Create CRDs for each resource to be deployed
state: draft
tags:
- multigateway
- multipooler
- multiorch
- toposerver
- crd
- kubebuilder
---

# Summary

Create individual CRDs for each Multigres component (`MultiGateway`, `MultiOrch`, `MultiPooler`, `Etcd`) and a parent `MultigresCluster` CRD. Each component CRD can be deployed independently or as part of a `MultigresCluster`. This provides flexibility for users to manage components individually while still supporting convenient cluster-level management.

This document focuses on defining the API types and CRD structure. Actual controller implementation and `MultigresCluster` parent-child creation logic are covered in separate documents.

# Motivation

Creating individual CRDs for each component provides several benefits:

1. **Flexibility**: Users can deploy only the components they need (e.g., just `MultiPooler` and `Etcd` without `MultiGateway`)
2. **Resource Sharing**: Multiple `MultigresCluster` instances can share common infrastructure components like `Etcd`
3. **Independent Lifecycle**: Components can be scaled, upgraded, or configured independently without affecting others
4. **Composability**: Advanced users can compose custom Multigres deployments by mixing and matching components
5. **Simplified Testing**: Each component can be tested in isolation during development

The `MultigresCluster` CRD provides a convenient higher-level abstraction for users who want to deploy a full Multigres stack with sensible defaults, while component CRDs give power users fine-grained control.

## Goals
- Define API types for all Multigres component CRDs
- Generate CRD manifests using kubebuilder
- Establish consistent field naming and structure across all CRDs
- Document resource relationships and ownership patterns
- Add validation markers for field constraints
- Create sample CR manifests for testing

## Non-Goals
- Controller implementation (covered in separate documents)
- Parent-child resource creation logic for `MultigresCluster` (separate document)
- Admission webhooks (using CRD validation only for now)
- Resource builder functions (covered in controller implementation docs)

# Proposal

Create the following CRD types in the `multigres.com/v1alpha1` API group:

1. **MultigresCluster** - Top-level resource representing a complete Multigres deployment
2. **MultiGateway** - Deployment configuration for the gateway component
3. **MultiOrch** - Deployment configuration for the orchestration component
4. **MultiPooler** - StatefulSet configuration for the connection pooler component
5. **Etcd** - StatefulSet configuration for the etcd cluster

## Resource Relationships

```
MultigresCluster (optional - creates others)
    ├── MultiGateway (can exist independently)
    ├── MultiOrch (can exist independently)
    ├── MultiPooler (can exist independently)
    └── Etcd (can exist independently)
```

Each component CRD can be created and managed independently, or via a `MultigresCluster` parent resource (parent-child relationship to be defined in separate document).

## Common Patterns

All component CRDs should follow these conventions:

- **Image Configuration**: `image` field with repository, tag, and pull policy
- **Replica Configuration**: `replicas` field (int32, with kubebuilder validation)
- **Resource Limits**: Standard Kubernetes `resources` field (ResourceRequirements)
- **HPA Support**: Optional `hpa` field for Horizontal Pod Autoscaler configuration
- **Scheduling Configuration**: Standard Kubernetes scheduling fields:
  - `affinity`: Pod affinity/anti-affinity and node affinity rules
  - `tolerations`: Tolerations for node taints
  - `nodeSelector`: Simple node selection by labels
  - `topologySpreadConstraints`: Control pod distribution across topology domains
- **Labels and Annotations**: Custom labels/annotations that merge with operator-generated ones

## Field Structure

### MultigresCluster Spec

```go
type MultigresClusterSpec struct {
    // Gateway configuration (optional)
    Gateway *MultiGatewaySpec `json:"gateway,omitempty"`

    // Orchestration configuration (optional)
    Orch *MultiOrchSpec `json:"orch,omitempty"`

    // Pooler configuration (optional)
    Pooler *MultiPoolerSpec `json:"pooler,omitempty"`

    // Etcd configuration (optional)
    Etcd *EtcdSpec `json:"etcd,omitempty"`
}
```

### Component Specs

Each component spec should include:

- **Image**: Container image configuration
- **Replicas**: Number of replicas (with validation: minimum 1, maximum configurable)
- **Resources**: CPU/memory requests and limits
- **HPA**: Optional horizontal pod autoscaler configuration
- **Affinity**: Optional pod affinity, anti-affinity, and node affinity rules
- **Tolerations**: Optional tolerations for node taints
- **NodeSelector**: Optional node selector labels
- **TopologySpreadConstraints**: Optional constraints for pod distribution across topology domains
- **Storage**: (For StatefulSet-based components like Etcd, MultiPooler)
- **Component-specific fields**: Any unique configuration needs

**Example Affinity Use Cases**:
- **Anti-affinity for HA**: Spread etcd replicas across different nodes/zones to survive node failures
- **Co-location**: Place MultiPooler near Postgres instances for low latency
- **Node isolation**: Dedicate specific nodes for database workloads using node affinity
- **Zone spreading**: Distribute replicas across availability zones using topology spread constraints

### Status Structure

Each CRD should have a status subresource with:

```go
type ComponentStatus struct {
    // Ready indicates if component is healthy and available
    Ready bool `json:"ready"`

    // Replicas is the desired number of replicas
    Replicas int32 `json:"replicas"`

    // ReadyReplicas is the number of ready replicas
    ReadyReplicas int32 `json:"readyReplicas"`

    // Conditions represent the latest observations of the component state
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // ObservedGeneration tracks which spec generation was reconciled
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}
```

# Design Details

## Directory Structure

```
api/v1alpha1/
├── groupversion_info.go       # API group registration
├── multigrescluster_types.go  # MultigresCluster CRD
├── multigateway_types.go      # MultiGateway CRD
├── multiorch_types.go         # MultiOrch CRD
├── multipooler_types.go       # MultiPooler CRD
├── etcd_types.go              # Etcd CRD
└── zz_generated.deepcopy.go   # Auto-generated (by kubebuilder)
```

## Kubebuilder Markers

Each CRD type file should include appropriate markers:

**For CRD generation**:
```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
```

**For validation**:
```go
// +kubebuilder:validation:Minimum=1
// +kubebuilder:validation:Maximum=100
// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
```

**For defaults**:
```go
// +kubebuilder:default=1
// +kubebuilder:default="postgres:16"
```

## Label Conventions

All CRDs and their managed resources should use consistent labels:

```yaml
labels:
  app.kubernetes.io/name: multigres
  app.kubernetes.io/instance: <cr-name>
  app.kubernetes.io/component: <gateway|orch|pooler|etcd>
  app.kubernetes.io/managed-by: multigres-operator
  multigres.com/cluster: <multigrescluster-name>  # If created by MultigresCluster
```

## Validation Strategy

Use CRD validation markers only (no admission webhooks at this stage):

- OpenAPI v3 schema constraints in kubebuilder markers
- Numeric ranges (min/max replicas, storage size)
- Enum values for service types, storage classes
- Pattern validation for image names
- Required vs optional field marking

## Implementation Tasks

1. Use `kubebuilder create api` to scaffold each CRD type
2. Define Go structs for Spec and Status of each type
3. Add kubebuilder markers for validation, defaults, print columns
4. Run `make manifests` to generate CRD YAML
5. Create sample CR manifests in `config/samples/`
6. Test CRD installation and validation
7. Document field meanings in godoc comments

## Test Plan

1. **CRD Generation**: Verify `make manifests` generates valid CRDs in `config/crd/bases/`
2. **Installation**: Test `kubectl apply -f config/crd/bases/` succeeds
3. **Validation**: Test invalid CR specs are rejected by API server (e.g., negative replicas, invalid enum values)
4. **kubectl Output**: Verify printer columns display correctly with sample CRs
5. **Schema Documentation**: Ensure `kubectl explain` shows field documentation for all CRDs
6. **Unit Tests**: Test that Go types have proper JSON tags and validation markers

## Version Skew Strategy

**Operator vs CRD Version**:
- Operator version must match or be newer than CRD version
- CRDs installed before operator deployment
- Operator can handle older CR specs (within same API version)

**Multiple Operators**:
- Not supported - use single operator with leader election
- Multiple replicas for HA use leader election (same version)

**CRD Versioning**:
- During alpha (v1alpha1), only one version supported at a time
- Future versions (beta/GA) can coexist with conversion webhooks

# Implementation History

- 2025-10-08: Initial draft created

# Drawbacks

**API Complexity**: Having individual CRDs for each component adds more API surface area compared to a single monolithic CRD.

**Learning Curve**: Users need to understand the relationship between `MultigresCluster` and component CRDs.

**Coordination**: When components need to reference each other, users must manage those references manually if not using `MultigresCluster`.

However, these drawbacks are outweighed by the flexibility and composability benefits for advanced use cases.

# Alternatives

## Alternative 1: Single Monolithic CRD

Create only a `MultigresCluster` CRD with all component specs embedded.

**Pros**:
- Simpler API surface
- Easier to understand for basic use cases
- Single resource to manage

**Cons**:
- No component reuse between clusters
- Cannot deploy components independently
- All-or-nothing deployment model
- Harder to scale/upgrade individual components

**Rejected because**: Lacks flexibility for advanced deployment patterns.

## Alternative 2: Component CRDs Only (No Parent)

Create only individual component CRDs without a `MultigresCluster` parent.

**Pros**:
- Maximum flexibility
- No abstraction layers

**Cons**:
- Verbose for simple deployments
- Users must manually create all components
- No convenience wrapper for common patterns

**Rejected because**: Makes basic deployments unnecessarily complex. The parent CRD provides important convenience.

## Alternative 3: Helm Charts Instead of CRDs

Use Helm charts to deploy Multigres components without operator.

**Pros**:
- Familiar deployment model for users
- No operator overhead

**Cons**:
- No automatic reconciliation
- No custom status reporting
- Limited to initial deployment
- Cannot react to cluster changes
- Helm templating syntax is difficult to maintain for complex logic
- Hard to test and debug template expansions
- Less type safety compared to Go structs

**Rejected because**: Operator pattern provides better lifecycle management and observability. Additionally, maintaining complex Helm templates is significantly harder for operator developers compared to writing Go code with proper type checking and testing.

# Infrastructure Needed

- **Kubebuilder**: For scaffolding and CRD generation
- **controller-gen**: For generating CRD manifests (included with kubebuilder)
- **Go toolchain**: Go 1.24+ for building API types
- **kubectl**: For testing CRD installation and validation
