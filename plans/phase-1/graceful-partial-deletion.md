---
title: Handle graceful deletion of part of Multigres cluster
state: draft
tags: [etcd, multigateway, multiorch, multipooler, lifecycle, reliability]
---

# Summary
Enable safe, graceful deletion or scaling-down of individual Multigres components while ensuring dependent components continue to operate correctly. Special focus on protecting etcd as the critical coordination infrastructure.

# Motivation
Users need flexibility to scale individual components independently without disrupting the entire Multigres cluster. Current implementation treats all components equally during deletion, but components have asymmetric dependencies - particularly etcd, which provides critical coordination services.

**Use Cases:**
1. Scale down MultiGateway replicas to zero for maintenance without affecting data plane (MultiPooler)
2. Remove MultiOrch temporarily while keeping connection pooling active
3. Scale individual components based on load patterns (e.g., more gateways during peak, fewer during off-hours)
4. Perform rolling component upgrades without full cluster downtime
5. Delete non-critical components while preserving etcd for remaining services

**Problems Solved:**
- Prevent accidental deletion of etcd while other components depend on it
- Enable partial cluster degradation instead of full outage
- Support flexible operational patterns (maintenance windows, cost optimization)
- Avoid cascading failures when removing individual components

## Goals
1. **Dependency-Aware Deletion**: Prevent deletion of etcd when other components still reference it
2. **Graceful Draining**: Allow components to complete in-flight operations before termination
3. **Independent Scaling**: Support scaling individual components to zero without affecting others
4. **Clear User Feedback**: Provide informative status conditions and events during partial deletion
5. **Protection Mechanisms**: Add safeguards for critical infrastructure components (etcd)
6. **Operational Flexibility**: Enable partial cluster operations for maintenance and optimization

## Non-Goals
1. **Automatic Dependency Resolution**: Not managing application-level startup order (Multigres components handle this)
2. **Data Migration**: Not handling data evacuation during deletion (handled by Multigres internals)
3. **Cross-Cluster Coordination**: Only managing single-cluster component lifecycle
4. **Application-Level Health**: Not validating Multigres-internal readiness beyond Kubernetes pod status
5. **Backup/Restore**: Not handling data backup before component deletion (separate concern)
6. **Multi-Tenant Isolation**: Not managing deletion conflicts across different Multigres instances (future work)

# Proposal

## Component Dependency Model

Define explicit dependency relationships between Multigres components:

```
etcd (critical infrastructure)
  ↑
  │ (may depend on)
  │
  ├─ MultiGateway (stateless, can scale to 0)
  ├─ MultiOrch (stateless, can scale to 0)
  └─ MultiPooler (stateful, can scale to 0 if no active connections)
```

**Dependency Rules:**
- **etcd**: No dependencies on other Multigres components. Can only be deleted when no other components reference it.
- **MultiGateway/MultiOrch/MultiPooler**: May depend on etcd for coordination (application-level concern, not operator-enforced).
- Components can reference etcd via spec fields (e.g., `etcdEndpoints` or similar).

## API Changes

### 1. Component Reference Tracking

Add optional etcd reference to component specs to enable dependency tracking:

```yaml
# MultiGateway example
apiVersion: multigres.com/v1alpha1
kind: MultiGateway
metadata:
  name: my-gateway
spec:
  etcdRef:
    name: my-etcd  # Reference to Etcd resource
  # ... other fields
```

This is **optional** - components can be deployed without etcd reference if they don't need coordination.

### 2. Deletion Protection Annotation

Add annotation to enable/disable deletion protection on etcd:

```yaml
apiVersion: multigres.com/v1alpha1
kind: Etcd
metadata:
  name: my-etcd
  annotations:
    multigres.com/deletion-protection: "enabled"  # or "disabled"
```

Default: `enabled` when other components reference this etcd instance.

### 3. Status Conditions

Add conditions to track deletion state and dependencies:

```yaml
status:
  conditions:
  - type: DeletionBlocked
    status: "True"
    reason: DependentComponentsExist
    message: "Cannot delete: 2 components still reference this etcd (my-gateway, my-pooler)"
  - type: SafeToDelete
    status: "False"
    reason: HasDependents
    message: "Remove or update dependent components first"
```

## Deletion Workflow

### Current Behavior (Baseline)
```go
// pkg/resource-handler/controller/etcd/etcd_controller.go:92-114
func (r *EtcdReconciler) handleDeletion(ctx, etcd) {
    if hasFinalizer {
        // No special cleanup - owner references handle resources
        removeFinalizer()
    }
}
```

### Proposed Enhanced Workflow

#### 1. Pre-Deletion Validation (in finalizer logic)

```go
func (r *EtcdReconciler) handleDeletion(ctx, etcd) {
    if hasFinalizer {
        // NEW: Check for dependent components
        dependents, err := r.findDependentComponents(ctx, etcd)
        if err != nil {
            return Result{}, err
        }

        // Block deletion if dependents exist and protection is enabled
        if len(dependents) > 0 && isDeletionProtectionEnabled(etcd) {
            r.updateDeletionBlockedCondition(ctx, etcd, dependents)
            r.emitEvent(etcd, Warning, "DeletionBlocked",
                fmt.Sprintf("Cannot delete: %d components depend on this etcd", len(dependents)))
            // Don't remove finalizer - keeps resource in "Terminating" state
            return Result{RequeueAfter: 30s}, nil
        }

        // NEW: Graceful shutdown delay for in-flight operations
        if !hasGracefulShutdownCompleted(etcd) {
            r.initiateGracefulShutdown(ctx, etcd)
            return Result{RequeueAfter: 10s}, nil
        }

        // Proceed with deletion
        removeFinalizer()
    }
}
```

#### 2. Dependent Component Discovery

```go
func (r *EtcdReconciler) findDependentComponents(ctx, etcd) ([]DependentComponent, error) {
    var dependents []DependentComponent
    namespace := etcd.Namespace

    // Check MultiGateway instances
    gateways := &multigresv1alpha1.MultiGatewayList{}
    if err := r.List(ctx, gateways, client.InNamespace(namespace)); err != nil {
        return nil, err
    }
    for _, gw := range gateways.Items {
        if gw.Spec.EtcdRef != nil && gw.Spec.EtcdRef.Name == etcd.Name {
            dependents = append(dependents, DependentComponent{
                Kind: "MultiGateway",
                Name: gw.Name,
            })
        }
    }

    // Check MultiOrch instances
    // ... similar logic

    // Check MultiPooler instances
    // ... similar logic

    return dependents, nil
}
```

#### 3. Graceful Shutdown for Other Components

For stateless components (Gateway, Orch):
```go
func (r *MultiGatewayReconciler) handleDeletion(ctx, gateway) {
    if hasFinalizer {
        // Set pod deletion grace period to allow connection draining
        if !hasGracePeriodElapsed(gateway) {
            r.updateDeletingCondition(ctx, gateway)
            // Wait for grace period (default: 30s)
            return Result{RequeueAfter: gracePeriod}, nil
        }

        removeFinalizer()
    }
}
```

For stateful components (Pooler):
```go
func (r *MultiPoolerReconciler) handleDeletion(ctx, pooler) {
    if hasFinalizer {
        // Check for active connections (via status or metrics)
        if hasActiveConnections(pooler) {
            r.updateDrainingCondition(ctx, pooler)
            r.emitEvent(pooler, Normal, "Draining", "Waiting for connections to close")
            return Result{RequeueAfter: 10s}, nil
        }

        removeFinalizer()
    }
}
```

## User Experience

### Scenario 1: Delete etcd with dependents

```bash
$ kubectl delete etcd my-etcd
etcd.multigres.com "my-etcd" deleted
# Resource enters Terminating state but doesn't disappear

$ kubectl get etcd my-etcd
NAME       READY   REPLICAS   AGE
my-etcd    true    3/3        5m    # Still exists, Terminating

$ kubectl describe etcd my-etcd
...
Status:
  Conditions:
    Type:    DeletionBlocked
    Status:  True
    Reason:  DependentComponentsExist
    Message: Cannot delete: 2 components depend on this etcd (my-gateway, my-pooler)
Events:
  Warning  DeletionBlocked  1s  etcd-controller  Cannot delete: 2 components depend on this etcd

# User must remove dependents first
$ kubectl delete multigateway my-gateway
$ kubectl delete multipooler my-pooler

# Now etcd deletion proceeds
$ kubectl get etcd my-etcd
Error from server (NotFound): etcds.multigres.com "my-etcd" not found
```

### Scenario 2: Force deletion with annotation

```bash
$ kubectl annotate etcd my-etcd multigres.com/deletion-protection=disabled
$ kubectl delete etcd my-etcd
# Deletion proceeds immediately, dependents will show errors
```

### Scenario 3: Scale component to zero

```bash
$ kubectl patch multigateway my-gateway --type=merge -p '{"spec":{"replicas":0}}'
# Gateway scales to 0, etcd remains operational
$ kubectl get pods
NAME                        READY   STATUS    RESTARTS   AGE
my-etcd-0                   1/1     Running   0          10m
my-etcd-1                   1/1     Running   0          10m
my-etcd-2                   1/1     Running   0          10m
my-pooler-0                 3/3     Running   0          10m
# Gateway pods gone, pooler still running
```

# Design Details

## Component-Specific Implementation

### Etcd Controller Changes

**File:** `pkg/resource-handler/controller/etcd/etcd_controller.go`

1. **Add dependency checking logic:**
   - New function `findDependentComponents()` - lists all components with `etcdRef` pointing to this instance
   - Requires RBAC permissions to list MultiGateway, MultiOrch, MultiPooler in same namespace

2. **Enhanced handleDeletion():**
   - Check annotation `multigres.com/deletion-protection` (default: enabled)
   - If enabled and dependents exist, update status condition and requeue without removing finalizer
   - Add metrics for blocked deletions
   - Emit events for visibility

3. **New status conditions:**
   - `DeletionBlocked` - True when dependents prevent deletion
   - `SafeToDelete` - True when no dependents and protection checks pass

4. **Graceful shutdown timing:**
   - Add annotation `multigres.com/graceful-shutdown-started` with timestamp
   - Wait minimum grace period (configurable, default 30s) before removing finalizer
   - Allow etcd cluster to complete ongoing operations

### MultiGateway Controller Changes

**File:** `pkg/resource-handler/controller/multigateway/multigateway_controller.go`

1. **Add etcdRef to spec:**
   ```go
   type MultiGatewaySpec struct {
       // EtcdRef references the Etcd instance for coordination
       // +optional
       EtcdRef *EtcdReference `json:"etcdRef,omitempty"`
       // ... existing fields
   }

   type EtcdReference struct {
       // Name of the Etcd resource in the same namespace
       Name string `json:"name"`
   }
   ```

2. **Enhanced handleDeletion():**
   - Add grace period for connection draining (default: 30s from pod terminationGracePeriodSeconds)
   - Update status condition: `Draining` while waiting
   - Operator doesn't enforce connection draining - relies on pod lifecycle and preStop hooks

3. **Validation:**
   - Add kubebuilder validation markers for etcdRef
   - Optional field - gateways can run without etcd if desired

### MultiOrch Controller Changes

**File:** `pkg/resource-handler/controller/multiorch/multiorch_controller.go`

Similar to MultiGateway:
- Add optional `etcdRef` field
- Simple grace period (stateless, no connection tracking)
- Standard pod termination handles cleanup

### MultiPooler Controller Changes

**File:** `pkg/resource-handler/controller/multipooler/multipooler_controller.go`

1. **Add etcdRef to spec** (similar to Gateway)

2. **Enhanced handleDeletion():**
   - More complex due to stateful nature (postgres, connections)
   - Check for active database connections via status field or metrics
   - Longer grace period (configurable, default: 60s)
   - Status condition: `ConnectionDraining`

3. **Connection tracking:**
   - Operator observes pod readiness/metrics, doesn't enforce application logic
   - Deletion proceeds after grace period regardless of connections
   - Users must handle connection draining via Multigres-internal mechanisms

## API Schema Changes

### New Types (in `api/v1alpha1/`)

```go
// EtcdReference identifies an Etcd instance
type EtcdReference struct {
    // Name of the Etcd resource
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`

    // Namespace of the Etcd resource (optional, defaults to same namespace)
    // +optional
    Namespace string `json:"namespace,omitempty"`
}

// DependentComponent describes a component that depends on another resource
type DependentComponent struct {
    Kind string
    Name string
    Namespace string
}
```

### Modified Types

Add to existing specs:
```go
// MultiGatewaySpec, MultiOrchSpec, MultiPoolerSpec all get:
type XxxSpec struct {
    // ... existing fields

    // EtcdRef optionally references an Etcd instance for coordination
    // +optional
    EtcdRef *EtcdReference `json:"etcdRef,omitempty"`
}
```

## Finalizer Logic Enhancement

### Current Pattern
```go
func (r *EtcdReconciler) handleDeletion(ctx, etcd) {
    if hasFinalizer {
        removeFinalizer()  // Simple, immediate
    }
}
```

### Enhanced Pattern
```go
func (r *EtcdReconciler) handleDeletion(ctx, etcd) {
    if hasFinalizer {
        // Step 1: Check protection
        if isDeletionProtectionEnabled(etcd) {
            dependents, err := r.findDependentComponents(ctx, etcd)
            if err != nil {
                return ctrl.Result{}, err
            }
            if len(dependents) > 0 {
                r.blockDeletion(ctx, etcd, dependents)
                return ctrl.Result{RequeueAfter: 30*time.Second}, nil
            }
        }

        // Step 2: Graceful shutdown
        if !r.hasGracefulShutdownCompleted(ctx, etcd) {
            r.initiateGracefulShutdown(ctx, etcd)
            return ctrl.Result{RequeueAfter: 10*time.Second}, nil
        }

        // Step 3: Remove finalizer
        removeFinalizer()
    }
}
```

### Graceful Shutdown Tracking

Use annotations for state tracking across reconciliation loops:

```go
const (
    gracefulShutdownStartedAnnotation = "multigres.com/graceful-shutdown-started"
    gracefulShutdownPeriodAnnotation = "multigres.com/graceful-shutdown-period"
)

func (r *EtcdReconciler) initiateGracefulShutdown(ctx, etcd) {
    if etcd.Annotations[gracefulShutdownStartedAnnotation] == "" {
        // First time - mark start
        period := getGracefulShutdownPeriod(etcd) // from annotation or default
        etcd.Annotations[gracefulShutdownStartedAnnotation] = time.Now().Format(time.RFC3339)
        etcd.Annotations[gracefulShutdownPeriodAnnotation] = period.String()
        r.Update(ctx, etcd)

        r.Recorder.Event(etcd, corev1.EventTypeNormal, "ShutdownStarted",
            fmt.Sprintf("Graceful shutdown started, waiting %s", period))
    }
}

func (r *EtcdReconciler) hasGracefulShutdownCompleted(ctx, etcd) bool {
    startedStr := etcd.Annotations[gracefulShutdownStartedAnnotation]
    if startedStr == "" {
        return false // Not yet started
    }

    started, err := time.Parse(time.RFC3339, startedStr)
    if err != nil {
        return false // Invalid timestamp, treat as not started
    }

    periodStr := etcd.Annotations[gracefulShutdownPeriodAnnotation]
    period, err := time.ParseDuration(periodStr)
    if err != nil {
        period = 30 * time.Second // Default
    }

    return time.Since(started) >= period
}
```

## Status Condition Management

### New Condition Types

```go
const (
    ConditionTypeDeletionBlocked = "DeletionBlocked"
    ConditionTypeSafeToDelete    = "SafeToDelete"
    ConditionTypeDraining        = "Draining"
)

func (r *EtcdReconciler) updateDeletionBlockedCondition(
    ctx context.Context,
    etcd *multigresv1alpha1.Etcd,
    dependents []DependentComponent,
) {
    dependentNames := []string{}
    for _, dep := range dependents {
        dependentNames = append(dependentNames, dep.Name)
    }

    condition := metav1.Condition{
        Type:               ConditionTypeDeletionBlocked,
        Status:             metav1.ConditionTrue,
        ObservedGeneration: etcd.Generation,
        LastTransitionTime: metav1.Now(),
        Reason:             "DependentComponentsExist",
        Message:            fmt.Sprintf("Cannot delete: %d components depend on this etcd (%s)",
                                      len(dependents), strings.Join(dependentNames, ", ")),
    }

    meta.SetStatusCondition(&etcd.Status.Conditions, condition)
    r.Status().Update(ctx, etcd)
}
```

## RBAC Changes

Add permissions for cross-component queries:

```go
// +kubebuilder:rbac:groups=multigres.com,resources=multigateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=multigres.com,resources=multiorchs,verbs=get;list;watch
// +kubebuilder:rbac:groups=multigres.com,resources=multipoolers,verbs=get;list;watch
```

Required in etcd controller to discover dependent components.

## Configuration Options

### Annotations

| Annotation | Component | Default | Purpose |
|------------|-----------|---------|---------|
| `multigres.com/deletion-protection` | Etcd | `enabled` | Enable/disable dependency checking |
| `multigres.com/graceful-shutdown-period` | All | `30s` | Time to wait before finalizer removal |
| `multigres.com/force-delete` | All | `false` | Skip all safety checks (dangerous) |

### Environment Variables (Operator-Wide)

```yaml
env:
- name: DEFAULT_GRACEFUL_SHUTDOWN_PERIOD
  value: "30s"
- name: ETCD_DELETION_PROTECTION_DEFAULT
  value: "enabled"
- name: MAX_GRACEFUL_SHUTDOWN_PERIOD
  value: "300s"  # Safety limit
```

## Metrics and Observability

### New Metrics

```go
// Deletion blocking metrics
deletionBlockedTotal := prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "multigres_deletion_blocked_total",
        Help: "Number of times deletion was blocked due to dependencies",
    },
    []string{"kind", "namespace"},
)

gracefulShutdownDuration := prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name: "multigres_graceful_shutdown_duration_seconds",
        Help: "Time taken for graceful shutdown before finalizer removal",
        Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
    },
    []string{"kind"},
)
```

### Events

| Event Type | Reason | Message Example |
|------------|--------|-----------------|
| Warning | DeletionBlocked | "Cannot delete: 2 components depend on this etcd (my-gateway, my-pooler)" |
| Normal | ShutdownStarted | "Graceful shutdown started, waiting 30s" |
| Normal | ShutdownComplete | "Graceful shutdown complete, proceeding with deletion" |
| Normal | Draining | "Waiting for connections to close" |
| Normal | SafeToDelete | "No dependent components found, safe to delete" |

## Test Plan

### Unit Tests

**Dependency Discovery:**
- `findDependentComponents()` with zero, one, and multiple dependents
- Cross-namespace references (should be rejected)
- Components without etcdRef (should not be counted)
- List errors and empty namespaces

**Protection Logic:**
- Blocking behavior when dependents exist
- Bypass with `deletion-protection=disabled` annotation
- Force delete with `force-delete=true` annotation
- Default behavior (protection enabled)

**Graceful Shutdown:**
- Annotation-based state tracking across reconcile loops
- Time calculation and comparison
- Invalid timestamps and missing annotations
- Custom grace periods via annotation

**Condition Management:**
- Building correct condition structures
- Merging with existing conditions
- Proper status/reason/message formatting

### Integration Tests (envtest)

**End-to-End Deletion Scenarios:**
1. Create Etcd + MultiGateway with etcdRef, attempt to delete Etcd
   - Verify: Etcd stays in Terminating state
   - Verify: DeletionBlocked condition appears
   - Verify: Event emitted with dependent name
2. Delete dependent MultiGateway
   - Verify: Etcd deletion proceeds and completes
3. Create Etcd + multiple dependents (Gateway, Orch, Pooler)
   - Verify: All listed in DeletionBlocked message
   - Delete one dependent at a time, verify count updates
4. Force deletion with annotation
   - Verify: Deletion proceeds immediately despite dependents

**Scaling Scenarios:**
1. Scale MultiGateway to 0 replicas while Etcd exists
   - Verify: Gateway pods removed, Etcd continues
2. Delete MultiGateway resource entirely
   - Verify: Different from scaling to zero
3. Recreate component after deletion
   - Verify: Can reference same Etcd instance

**Race Conditions:**
1. Concurrent deletion of Etcd and dependent component
   - Verify: No finalizer deadlock
2. Rapid creation/deletion cycles
   - Verify: Proper cleanup, no leaked resources

**Grace Period Behavior:**
1. Default grace period (30s) applied correctly
2. Custom grace period via annotation
3. Multiple reconcile loops during grace period
   - Verify: Annotation timestamp preserved
   - Verify: RequeueAfter timing correct

## Implementation Milestones

### MVP (Minimal Viable Product)
**Scope:**
- Basic dependency tracking for etcd only
- Deletion blocking with status conditions
- Simple annotation-based protection controls
- Default grace periods (no customization initially)

**Success Criteria:**
- Etcd cannot be deleted while components reference it
- Clear error messages guide users to fix dependencies
- Works in local kind cluster testing
- Core unit and integration tests pass

**Deliverables:**
- Enhanced etcd controller with dependency checking
- `etcdRef` field added to component specs
- DeletionBlocked status condition
- Basic documentation and examples

### Phase 2 (Enhanced Graceful Shutdown)
**Scope:**
- Configurable grace periods via annotations
- Connection draining for MultiPooler
- Enhanced observability (metrics, detailed events)
- Graceful shutdown for all component types

**Success Criteria:**
- Custom grace periods work correctly
- Metrics track deletion blocking and shutdown duration
- Users can monitor deletion progress via kubectl
- Production-ready error handling

**Deliverables:**
- Grace period configuration options
- Prometheus metrics integration
- Enhanced status conditions and events
- User documentation for operational patterns

### Phase 3 (Advanced Features)
**Scope:**
- Automatic dependency detection improvements
- Cross-namespace references (if needed)
- Force deletion safeguards and confirmation
- Performance optimization for large clusters

**Success Criteria:**
- Handles complex multi-component clusters efficiently
- Dependency discovery performs well at scale
- Security review completed
- Production validation from users

# Implementation History
- 2025-10-16: Initial draft created with comprehensive specification

# Drawbacks

## Increased Complexity
- Adds significant logic to deletion workflow (finalizer complexity)
- More state tracking via annotations
- Additional RBAC permissions for cross-component queries
- More surface area for bugs in edge cases

## Potential for Stuck Resources
- If dependency tracking has bugs, resources could get stuck in Terminating state
- Requires manual intervention (force delete annotation) to recover
- Users may not understand why deletion is blocked

## API Expansion
- New fields in all component specs (`etcdRef`)
- More annotations to document and maintain
- Could confuse users if not well documented

## Performance Considerations
- Dependency discovery requires listing all components in namespace
- Could be slow in namespaces with many resources
- Repeated reconciliation during grace periods adds load

## Breaking Change Risk
- Changes deletion behavior - could surprise existing users
- Force delete annotation is dangerous if misused
- Need clear migration path and documentation

# Alternatives

## Alternative 1: No Deletion Protection (Current State)

**Approach:** Let Kubernetes handle deletion naturally, no operator intervention

**Pros:**
- Simple, no additional code
- Kubernetes owner references handle cleanup automatically
- No performance overhead

**Cons:**
- Users can accidentally delete etcd while components depend on it
- No graceful shutdown - immediate termination
- Dependent components will error when etcd disappears
- No user guidance on proper deletion order

**Decision:** Rejected - users need protection from accidental etcd deletion

## Alternative 2: Admission Webhook for Validation

**Approach:** Use ValidatingWebhook to block etcd deletion at API level

**Pros:**
- Prevents deletion before it starts (no Terminating state)
- Immediate feedback to user
- Centralized validation logic

**Cons:**
- Requires webhook infrastructure (certificates, endpoints)
- Adds operational complexity
- Harder to test (webhook server setup)
- Can't do asynchronous checks or grace periods

**Decision:** Rejected for MVP - too much infrastructure overhead. Could be added later.

## Alternative 3: Owner References from Components to Etcd

**Approach:** Make components owners of etcd, Kubernetes blocks deletion automatically

**Pros:**
- Leverages native Kubernetes ownership model
- No custom code needed
- Automatic protection

**Cons:**
- Backwards ownership (components depend on etcd, not vice versa)
- Doesn't support graceful shutdown
- Hard to override for force deletion
- Confusing ownership semantics

**Decision:** Rejected - violates logical dependency model

## Alternative 4: External Dependency Manager

**Approach:** Separate controller/tool that manages dependencies between resources

**Pros:**
- Reusable across different resource types
- Clear separation of concerns
- Could be published as standalone project

**Cons:**
- Additional component to deploy and maintain
- More complex architecture
- Still needs integration with each controller
- Overkill for this use case

**Decision:** Rejected - too complex for current needs

## Alternative 5: Finalizer-Based with Status-Only Tracking

**Approach:** Similar to proposal but track dependents in status instead of discovering them

**Pros:**
- No need to list components in namespace (better performance)
- Dependencies pre-cached in status

**Cons:**
- Status can become outdated (spec is source of truth)
- Requires components to update etcd status when they reference it
- More complex coordination between controllers
- Race conditions if status updates fail

**Decision:** Rejected - spec-based discovery is more reliable

## Selected Approach: Finalizer-Based Dependency Discovery (Proposal)

**Why chosen:**
- Balances safety with implementation complexity
- Works within existing Kubernetes patterns (finalizers)
- Provides clear user feedback via conditions and events
- Allows force override when needed
- No additional infrastructure (webhooks, external tools)
- Can be implemented incrementally (MVP → enhanced features)

# Infrastructure Needed

## Development Infrastructure

**Local Testing:**
- kind cluster (already in use)
- kubectl access
- No additional infrastructure

**CI/CD:**
- Existing GitHub Actions pipeline
- envtest for integration tests (already available)
- No new external dependencies

## Runtime Infrastructure

**Kubernetes Cluster Requirements:**
- No additional CRDs beyond existing Multigres resources
- Standard RBAC permissions (list/watch additional resource types)
- No webhooks required for MVP
- No external services or databases

**Operator Requirements:**
- Event recorder (already available via controller-runtime)
- Leader election (already configured)
- Metrics endpoint (existing OpenTelemetry integration)

## Optional Infrastructure (Future Phases)

**Phase 2+:**
- Prometheus for metrics visualization (optional)
- Grafana dashboards for deletion monitoring (optional)
- Admission webhooks for pre-deletion validation (requires cert-manager or manual certs)

## Documentation Infrastructure

**Required:**
- Air document (this document)
- User guide examples in `config/samples/`
- Godoc comments for new API fields
- CHANGELOG entry

**Optional:**
- User documentation website updates
- Blog post or announcement for new feature
- Video demo of graceful deletion workflow

