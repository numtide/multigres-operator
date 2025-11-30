---
title: GitOps Integration with Argo CD (and Flux)
state: draft
tags:
- gitops
- argocd
- flux
- deployment
- testing
---

# Summary

Ensure the Multigres operator is GitOps-friendly, with primary focus on Argo CD integration (client requirement) and secondary support for Flux CD (community use). Establish clear ownership boundaries between GitOps tools (which manage Custom Resources) and the operator (which manages child Kubernetes resources). Prevent resource conflicts when multiple MultigresCluster, MultiGateway, MultiOrch, MultiPooler, and Etcd CRs are deployed.

# Motivation

**Primary Driver**: The client uses Argo CD for all Kubernetes deployments and requires the Multigres operator to work seamlessly within their GitOps workflow.

**Secondary Goal**: Community users may prefer Flux CD, so we should ensure basic compatibility without extensive Flux-specific testing initially.

Without proper GitOps integration, users face several challenges:

1. **Resource Ownership Conflicts**: GitOps tools and the operator may compete to manage the same resources, causing reconciliation loops
2. **Drift Detection**: GitOps tools may incorrectly flag operator-managed resources as "out of sync"
3. **Deletion Safety**: Improper configuration could cause GitOps to prematurely delete resources the operator still needs
4. **Multi-CR Deployments**: With multiple Multigres CRs in the same namespace, label selectors and resource tracking become critical

Without testing GitOps integration, the client cannot deploy Multigres in their production environment.

## Goals

- **Argo CD Integration (Priority 1)**: Full testing and validation with Argo CD
- **Clear Ownership Model**: Define which resources are managed by GitOps vs the operator
- **Multi-CR Support**: Ensure multiple Multigres CRs can coexist without conflicts in Argo CD
- **Argo CD Documentation**: Comprehensive deployment guides and best practices for Argo CD
- **Drift Prevention**: Configure Argo CD to ignore operator-managed resources
- **Flux Compatibility (Priority 2)**: Basic validation that Flux works, minimal documentation

## Non-Goals

- **Extensive Flux Testing**: Basic validation only; deep Flux integration is future work
- **GitOps Tool Development**: We're testing integration, not developing GitOps features
- **Helm Chart Deployment**: This focuses on direct CR deployment via GitOps (Helm is covered separately)
- **Multi-Cluster GitOps**: Focus on single-cluster deployments first
- **Advanced GitOps Patterns**: Progressive delivery, canary deployments are future enhancements

# Proposal

## Parent-Child Architecture

The Multigres operator uses a two-level CR hierarchy:

```
MultigresCluster (Parent CR)
    ├── Creates → MultiGateway CR (Child CR)
    ├── Creates → MultiOrch CR (Child CR)
    ├── Creates → MultiPooler CR (Child CR)
    └── Creates → Etcd CR (Child CR)

Each Child CR
    └── Creates → Kubernetes resources (Deployment/StatefulSet, Service, HPA, PVC, etc.)
```

**Deployment Patterns**:
1. **Parent CR only**: `MultigresCluster` creates and manages all child CRs (traditional operator pattern)
2. **Child CRs directly**: Skip `MultigresCluster`, deploy individual CRs via GitOps
3. **Hybrid with `managed` flag**: Start with parent, set `managed: false` to hand off to GitOps

## The `managed` Flag

Each component in `MultigresCluster` spec has a `managed` boolean flag controlling parent-child relationship:

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: production-cluster
  namespace: multigres
spec:
  gateway:
    managed: true  # Default - parent controls this child CR
    replicas: 3

  etcd:
    managed: false # Child CR is independent - GitOps manages it
    replicas: 3

  orch:
    managed: true  # Parent controls this
    replicas: 2

  pooler:
    managed: false # Child CR is independent
    replicas: 3
```

### `managed` Flag Behavior

**When `managed: true` (Default - Parent Controls Child)**:
- Parent creates child CR with owner reference
- Parent spec changes propagate to child spec
- Deleting parent deletes child CRs (cascade via owner references)
- GitOps manages only the parent CR

**When `managed: false` (Child Becomes Independent)**:
- Parent removes owner reference from child CR
- Child CR continues to exist independently
- Parent no longer updates child spec
- GitOps can manage child CR directly without conflicts
- Deleting parent does NOT delete unmanaged child CRs

**When switching `managed: false → true` (Reclaim Child)**:
- Parent re-adds owner reference to child CR
- Parent resumes managing child spec
- May reclaim orphaned StatefulSets/Deployments

## Three Levels of Owner References

### Level 1: MultigresCluster → Child CRs (Controlled by `managed` flag)

**When `managed: true`**:
```yaml
apiVersion: multigres.com/v1alpha1
kind: MultiGateway
metadata:
  name: production-cluster-gateway
  ownerReferences:
  - kind: MultigresCluster
    name: production-cluster
    controller: true
```

**When `managed: false`**:
```yaml
apiVersion: multigres.com/v1alpha1
kind: MultiGateway
metadata:
  name: production-cluster-gateway
  ownerReferences: []  # No parent owner reference
  annotations:
    multigres.com/unmanaged-from: production-cluster
    multigres.com/unmanaged-at: "2025-10-16T10:30:00Z"
```

### Level 2: Child CR → Kubernetes Resources (Always Set)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: production-cluster-gateway
  labels:
    app.kubernetes.io/managed-by: multigres-operator
    app.kubernetes.io/instance: production-cluster-gateway
  ownerReferences:
  - kind: MultiGateway
    name: production-cluster-gateway
    controller: true
```

**Critical**: Child CRs always set owner references on their Kubernetes resources, regardless of `managed` flag value.

### Level 3: StatefulSet → Pods (Kubernetes Native)

Standard Kubernetes behavior - StatefulSets always own their Pods.

## StatefulSet Orphaning for Zero-Downtime Transitions

### The Problem

When setting `managed: false` for StatefulSet-based components (Etcd, MultiPooler), we risk Pod deletion:

**Dangerous Sequence** (Would Kill Pods):
1. Remove owner reference from `Etcd` CR
2. User deletes `Etcd` CR
3. Kubernetes garbage collection deletes StatefulSet
4. StatefulSet deletion deletes all Pods and PVCs
5. **Database downtime and data loss!**

**Safe Sequence** (Orphan StatefulSet):
1. Mark StatefulSet as orphaned before removing owner reference
2. Remove owner reference from `Etcd` CR
3. Even if `Etcd` CR is deleted, StatefulSet survives
4. GitOps can recreate `Etcd` CR and reclaim orphaned StatefulSet
5. **Zero downtime!**

### Orphaning Mechanism

Child controllers (Etcd, MultiPooler) must support orphaning via annotations:

**Trigger**: Parent sets annotation on child CR:
```yaml
annotations:
  multigres.com/orphan-statefulset: "true"
```

**Response**: Child controller orphans its StatefulSet:
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: production-cluster-etcd
  annotations:
    multigres.com/orphan-policy: "retain"
    multigres.com/orphaned-from: production-cluster-etcd
    multigres.com/orphaned-at: "2025-10-16T10:30:00Z"
  ownerReferences: []  # Owner reference removed
```

Now the StatefulSet survives even if the Etcd CR is deleted.

### Orphaning Workflow

**Step 1**: User sets `managed: false` in MultigresCluster spec

**Step 2**: Parent controller adds orphan annotation to child CR:
```yaml
annotations:
  multigres.com/orphan-statefulset: "true"
```

**Step 3**: Child controller sees annotation and orphans StatefulSet (removes owner reference, adds orphan annotations)

**Step 4**: Parent removes owner reference from child CR (`managed: false` complete)

**Step 5**: Child CR is now independent, GitOps can manage it

**Step 6**: If child CR is later deleted, StatefulSet and Pods survive

## Resource Reclaiming

When a child CR is recreated (e.g., via GitOps), it should reclaim its orphaned StatefulSet:

**Scenario**:
1. Etcd CR deleted (StatefulSet orphaned, survives)
2. User recreates Etcd CR via GitOps with same name
3. Child controller detects orphaned StatefulSet with matching name
4. Controller verifies spec compatibility
5. Controller adopts orphaned StatefulSet (re-adds owner reference)
6. StatefulSet now owned by new CR, Pods never restarted

**Spec Compatibility Check**: Operator must verify that orphaned StatefulSet spec matches new CR spec (replicas, storage size, images). If incompatible, fail with clear error requiring manual intervention.

## Argo CD Configuration

### Approach 1: Global Exclusion (Simplest)

Configure Argo CD globally to ignore all operator-managed Kubernetes resources:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
  namespace: argocd
data:
  resource.customizations.ignoreDifferences.all: |
    jqPathExpressions:
    - .metadata.ownerReferences

  resource.exclusions: |
    - apiGroups:
      - apps
      kinds:
      - Deployment
      - StatefulSet
      clusters:
      - "*"
      jqPathExpressions:
      - .metadata.labels["app.kubernetes.io/managed-by"] == "multigres-operator"

    - apiGroups:
      - ""
      kinds:
      - Service
      - PersistentVolumeClaim
      clusters:
      - "*"
      jqPathExpressions:
      - .metadata.labels["app.kubernetes.io/managed-by"] == "multigres-operator"
```

**Pros**: Single configuration, applies to all Applications
**Cons**: Global scope may affect other workloads

### Approach 2: Per-Application Configuration (Recommended)

Configure exclusions per Argo CD Application:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: multigres-production
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/company/gitops-repo
    targetRevision: main
    path: apps/production
  destination:
    server: https://kubernetes.default.svc
    namespace: multigres
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
  ignoreDifferences:
  - group: apps
    kind: Deployment
    jqPathExpressions:
    - .metadata.ownerReferences
  - group: apps
    kind: StatefulSet
    jqPathExpressions:
    - .metadata.ownerReferences
  - group: ""
    kind: Service
    jqPathExpressions:
    - .metadata.ownerReferences
```

**Pros**: Scoped per Application, no global impact
**Cons**: Must configure each Application

## Deployment Patterns with Argo CD

### Pattern 1: Fully Managed (Parent Only)

**Git Repo**:
```
gitops-repo/
└── apps/production/
    └── multigres-cluster.yaml
```

**multigres-cluster.yaml**:
```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: production-cluster
spec:
  gateway:
    managed: true  # All components managed by parent
    replicas: 3
  etcd:
    managed: true
    replicas: 3
  orch:
    managed: true
    replicas: 2
  pooler:
    managed: true
    replicas: 3
```

**Result**: Argo CD manages only parent CR. Operator manages all child CRs and Kubernetes resources.

### Pattern 2: Hybrid (Mixed Managed/Unmanaged)

**Git Repo**:
```
gitops-repo/
└── apps/production/
    ├── multigres-cluster.yaml  # Parent with managed flags
    ├── etcd.yaml               # Unmanaged child
    └── pooler.yaml             # Unmanaged child
```

**multigres-cluster.yaml**:
```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: production-cluster
spec:
  gateway:
    managed: true   # Parent manages
  etcd:
    managed: false  # GitOps manages
  orch:
    managed: true   # Parent manages
  pooler:
    managed: false  # GitOps manages
```

**etcd.yaml** (GitOps controls spec):
```yaml
apiVersion: multigres.com/v1alpha1
kind: Etcd
metadata:
  name: production-cluster-etcd
spec:
  replicas: 5       # Different from parent default
  storage:
    size: 100Gi
```

**Result**: GitOps manages parent + unmanaged child CRs. Parent only controls managed children.

### Pattern 3: Child CRs Only (No Parent)

**Git Repo**:
```
gitops-repo/
└── apps/production/
    ├── gateway.yaml
    ├── etcd.yaml
    ├── orch.yaml
    └── pooler.yaml
```

**Result**: No parent CR. All child CRs managed independently by GitOps.

## Flux CD Configuration (Basic Support)

Flux respects owner references automatically, minimal configuration needed:

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: multigres-production
  namespace: flux-system
spec:
  interval: 5m
  path: ./apps/production
  prune: true
  sourceRef:
    kind: GitRepository
    name: gitops-repo
  healthChecks:
  - apiVersion: multigres.com/v1alpha1
    kind: MultigresCluster
    name: production-cluster
    namespace: multigres
  wait: true
```

**Key Point**: Flux won't prune resources with owner references pointing to tracked CRs. No special exclusion config required.

# Design Details

## API Changes

Add `managed` field to each component spec in MultigresCluster:

```go
type MultigresClusterSpec struct {
    Gateway *ManagedMultiGatewaySpec `json:"gateway,omitempty"`
    Etcd    *ManagedEtcdSpec         `json:"etcd,omitempty"`
    Orch    *ManagedMultiOrchSpec    `json:"orch,omitempty"`
    Pooler  *ManagedMultiPoolerSpec  `json:"pooler,omitempty"`
}

type ManagedEtcdSpec struct {
    // Managed controls whether parent manages this child CR
    // +kubebuilder:default=true
    Managed bool `json:"managed"`

    // Embed component spec
    EtcdSpec `json:",inline"`
}
```

## Implementation Requirements

### Parent Controller (`MultigresCluster`)

**Responsibilities**:
1. Create child CRs with owner references when `managed: true`
2. Remove owner references when transitioning `managed: true → false`
3. Add orphan annotation to child CR before removing owner reference
4. Re-add owner references when transitioning `managed: false → true`
5. Sync parent spec to child spec only when `managed: true`
6. Never touch child spec when `managed: false`

**Key Transition Logic**:
- `managed: true → false`: Annotate child → Wait → Remove owner ref → Add tracking annotations
- `managed: false → true`: Add owner ref → Remove orphan annotation → Update tracking

### Child Controllers (`Etcd`, `MultiPooler`)

**Responsibilities**:
1. Always set owner references on Kubernetes resources (Deployments, StatefulSets, Services)
2. Detect orphan annotation (`multigres.com/orphan-statefulset: "true"`)
3. Orphan StatefulSet when annotation present (remove owner ref, add orphan annotations)
4. Reclaim orphaned StatefulSets when creating new CR with matching name
5. Verify spec compatibility before reclaiming
6. Support both cascade deletion (normal) and orphan deletion (when flagged)

**Key Orphaning Logic**:
- Check for orphan annotation on reconcile
- If deleting with orphan annotation: Remove StatefulSet owner ref → Add orphan annotations → Allow CR deletion
- If creating and orphaned StatefulSet exists: Verify compatibility → Re-add owner ref → Remove orphan annotations

## Test Plan

### Unit Tests

**Coverage Goal**: 100% for all managed flag and orphaning logic

**Parent Controller Tests**:
- Managed flag transitions (true→false, false→true)
- Owner reference management
- Spec propagation (managed vs unmanaged)
- Parent deletion behavior (cascade vs preserve)

**Child Controller Tests**:
- Orphan annotation detection
- StatefulSet orphaning
- Orphaned resource reclaiming
- Spec compatibility validation
- Cascade vs orphan deletion

### Integration Tests (envtest)

**Test Scenarios**:

1. **Managed Flag Lifecycle**
   - Create MultigresCluster with managed: true
   - Verify child CR has owner reference
   - Set managed: false
   - Verify owner reference removed
   - Set managed: true again
   - Verify owner reference re-added

2. **StatefulSet Orphaning**
   - Create Etcd CR
   - Verify StatefulSet created with owner reference
   - Add orphan annotation to Etcd CR
   - Delete Etcd CR
   - Verify StatefulSet survives with orphan annotations
   - Verify owner reference removed from StatefulSet

3. **Pod Survival During Transitions**
   - Create MultigresCluster with Etcd (managed: true)
   - Record Pod UIDs
   - Transition to managed: false
   - Verify same Pods exist (UIDs unchanged)
   - Delete parent
   - Verify Pods still exist

4. **Resource Reclaiming**
   - Create orphaned StatefulSet
   - Create Etcd CR with matching name
   - Verify StatefulSet reclaimed (owner ref added)
   - Verify orphan annotations removed

5. **Multi-CR Coexistence**
   - Deploy 3 MultigresCluster CRs in same namespace
   - Verify no resource naming conflicts
   - Verify label-based resource selection works
   - Transition one to managed: false
   - Verify others unaffected

### End-to-End Tests with Argo CD

**Test Infrastructure**:
- Kind cluster
- Argo CD installed
- Git repository with test manifests
- Automated via bash scripts in `test/e2e/`

**Test Scenarios**:

1. **Basic Argo CD Sync**
   - Deploy MultigresCluster via Argo Application
   - Verify Application shows Synced and Healthy
   - Verify child CRs created by operator
   - Verify Argo CD doesn't show operator resources as drift

2. **Managed Flag Transition via GitOps**
   - Deploy with managed: true
   - Commit change to managed: false
   - Sync Application
   - Verify owner references removed
   - Verify Pods survived
   - Delete parent via Git
   - Verify child CRs survived

3. **Multi-CR Deployment**
   - Deploy 3 MultigresCluster CRs via Argo
   - Verify all succeed without conflicts
   - Transition one to managed: false
   - Verify no cross-CR interference

4. **Orphan and Reclaim**
   - Deploy Etcd via GitOps (managed: false)
   - Delete Etcd from Git
   - Verify StatefulSet orphaned
   - Re-add Etcd to Git
   - Verify StatefulSet reclaimed

5. **Conflict Detection**
   - Deploy via GitOps
   - Manually modify child resource
   - Verify operator reconciles
   - Verify no Argo CD drift warnings

**Test Script Example**:
```bash
#!/bin/bash
# test/e2e/argocd-managed-lifecycle.sh

# Setup cluster and install operator
kind create cluster
make deploy

# Install Argo CD
kubectl create ns argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/.../install.yaml

# Deploy test Application
kubectl apply -f test/e2e/test-application.yaml

# Wait for sync
kubectl wait --for=condition=Synced app/test -n argocd

# Run test assertions
./test/e2e/verify-resources.sh

# Cleanup
kind delete cluster
```

### Flux Basic Validation

**Minimal Testing**:
- Deploy MultigresCluster via Flux Kustomization
- Verify reconciliation works
- Verify pruning doesn't delete operator resources
- Document basic Flux configuration

**Not Testing**:
- Advanced Flux features
- Extensive Flux-specific scenarios
- Flux GitOps patterns

## Success Criteria

### MVP Requirements

**Functionality**:
- ✅ `managed` flag implemented and working
- ✅ Owner references correctly managed
- ✅ StatefulSet orphaning prevents Pod deletion
- ✅ Orphaned resources can be reclaimed
- ✅ Multi-CR deployments work without conflicts
- ✅ Argo CD integration tested and documented
- ✅ Flux basic compatibility validated

**Testing**:
- ✅ Unit tests: 100% coverage for managed flag and orphaning logic
- ✅ Integration tests: All critical scenarios passing
- ✅ E2E tests: Argo CD lifecycle tests automated
- ✅ Manual testing: Production-like scenarios validated

**Documentation**:
- ✅ Argo CD deployment guide with examples
- ✅ `managed` flag behavior documented
- ✅ Troubleshooting guide for common issues
- ✅ GitOps best practices documented

### Future Enhancements

**Post-MVP**:
- Advanced Argo CD patterns (ApplicationSets, multi-cluster)
- Enhanced Flux integration and testing
- Automated orphan cleanup (garbage collection)
- Conflict resolution strategies for reclaiming modified resources
- Progressive delivery patterns
- Multi-cluster GitOps support

## Upgrade / Downgrade Strategy

**Adding `managed` Flag to Existing Deployments**:
- Default value: `managed: true` (maintains current behavior)
- Existing CRs inherit default automatically
- No breaking changes
- Users opt-in to `managed: false`

**Downgrade Considerations**:
- Older operator versions ignore `managed` field (standard K8s behavior)
- Downgrading loses managed flag functionality
- Child CRs remain functional
- Document: Downgrade returns to fully-managed behavior

## Version Skew Strategy

**Operator vs CRD Version**:
- CRD must be upgraded before operator
- Operator checks CRD version on startup
- Fails fast if CRD version is too old

**Multiple Operator Replicas**:
- Use leader election (controller-runtime default)
- Only leader reconciles CRs
- Safe for HA deployments
- All replicas must be same version

**GitOps Tool Version**:
- Operator agnostic to Argo CD/Flux version
- Uses standard Kubernetes owner references
- Works with any GitOps tool respecting owner references

# Production Readiness Review Questionnaire

## Feature Enablement and Rollback

**Feature Enablement**:
- `managed` flag in MultigresCluster spec (no feature gate needed)
- Default: `managed: true` (opt-in to `managed: false`)
- No impact on existing deployments

**Rollback**:
- Set `managed: true` to return to parent control
- Reclaiming may require manual intervention if child specs diverged
- Document rollback procedures

## Rollout, Upgrade and Rollback Planning

**Rollout Strategy**:
1. Deploy new CRDs with `managed` field
2. Deploy updated operator
3. Existing clusters continue working (`managed: true` default)
4. Users opt-in to `managed: false` per component

**Upgrade Path**:
- No migration required
- Backward compatible with existing CRs
- Users adopt new patterns incrementally

**Rollback Plan**:
- Downgrade operator (loses `managed` flag functionality)
- Child CRs remain functional
- All children become parent-managed again

## Monitoring Requirements

### Metrics

Expose Prometheus metrics for GitOps integration:

```
multigres_managed_transitions_total{cluster, component, from, to}
multigres_orphaned_statefulsets{namespace}
multigres_reclaim_attempts_total{namespace, statefulset, success}
multigres_reconciliation_duration_seconds{controller}
```

### Alerts

```yaml
- alert: MultigresOrphanedStatefulSetDetected
  expr: multigres_orphaned_statefulsets > 0
  for: 1h

- alert: MultigresReclaimFailures
  expr: rate(multigres_reclaim_attempts_total{success="false"}[5m]) > 0
```

## Dependencies

<!-- TODO: Check with client which Argo CD version they are using (could be v3.x or lower) -->

- **Argo CD**: TBD - need to confirm with client (possibly v3.0, v3.1, or lower)
- **Flux**: v2.7 (community support for basic validation)
- **Kubernetes**: v1.32, v1.33, v1.34
- **controller-runtime**: Owner reference APIs

## Scalability

- **Number of CRs**: Operator should handle 100+ MultigresCluster CRs per namespace
- **Managed Flag Transitions**: Process within 10 seconds
- **Orphan Cleanup**: Background job to clean up orphaned resources older than 7 days (future enhancement)
- **GitOps Sync Load**: Compatible with Argo CD sync intervals of 1-5 minutes

## Troubleshooting

### Issue: Child CR Not Released When `managed: false`

**Symptom**: Owner reference remains after setting managed: false

**Debug**:
```bash
# Check parent controller logs
kubectl logs -n multigres-operator-system deployment/multigres-operator | grep "Releasing control"

# Check child CR annotations
kubectl get etcd <name> -o yaml | grep -A5 annotations
```

**Solution**:
- Verify parent controller is running
- Check for errors in parent controller logs
- Manually remove owner reference if stuck:
```bash
kubectl patch etcd <name> --type json -p '[{"op": "remove", "path": "/metadata/ownerReferences"}]'
```

### Issue: Pods Deleted During `managed` Transition

**Symptom**: StatefulSet Pods recreated during managed flag change

**Root Cause**: StatefulSet not orphaned before owner reference removal

**Debug**:
```bash
# Check if orphan annotation was set
kubectl get statefulset <name> -o yaml | grep "multigres.com/orphan"

# Check child controller logs
kubectl logs -n multigres-operator-system deployment/multigres-operator | grep orphan
```

**Prevention**:
- Ensure child controller has finalizer logic
- Wait period between orphan annotation and owner reference removal
- Monitor Pod UIDs during transitions (should not change)

### Issue: Orphaned StatefulSet Not Reclaimed

**Symptom**: New child CR created but doesn't adopt orphaned StatefulSet

**Debug**:
```bash
# Check if StatefulSet is orphaned
kubectl get statefulset <name> -o yaml | grep "orphan-policy"

# Check spec compatibility
kubectl get statefulset <name> -o yaml > sts.yaml
kubectl get etcd <name> -o yaml > etcd.yaml
# Compare specs manually
```

**Solution**:
- Verify CR name matches original owner
- Check spec compatibility (replicas, storage, images must match)
- If specs incompatible, delete orphaned StatefulSet and recreate:
```bash
kubectl delete statefulset <name> --cascade=orphan
kubectl rollout restart etcd <name>
```

### Issue: Argo CD Shows Resources as "Out of Sync"

**Symptom**: Argo CD UI shows Deployments/StatefulSets as drift

**Solution**:
- Verify Argo CD configured to ignore operator-managed resources
- Check Application ignoreDifferences configuration:
```bash
kubectl get application <name> -n argocd -o yaml | grep -A10 ignoreDifferences
```
- Add resource exclusions to argocd-cm ConfigMap

### Issue: Multiple CRs Conflict on Resource Names

**Symptom**: Error creating child resources due to name conflicts

**Debug**:
```bash
# Check for duplicate resource names
kubectl get all -l app.kubernetes.io/managed-by=multigres-operator

# Check CR naming
kubectl get multigrescluster,etcd,multigateway -A
```

**Solution**:
- Ensure each MultigresCluster has unique name
- Operator uses `{cr-name}-{component}` naming pattern
- Use namespace isolation for complete separation

# Implementation History

- 2025-10-16: Initial draft created

# Drawbacks

**Complexity**: The `managed` flag and orphaning logic add significant complexity compared to simple owner references.

**Testing Burden**: Extensive testing required for all transition scenarios (managed true→false, false→true, orphan, reclaim).

**Potential for User Error**: Users may accidentally set `managed: false` and lose parent control unintentionally.

**StatefulSet Orphaning Risk**: If orphaning logic fails, Pods could be deleted during transitions.

However, these drawbacks are necessary to support production GitOps workflows where users need fine-grained control over resource management.

# Alternatives

## Alternative 1: No Parent CR (Child CRs Only)

Deploy all components as individual CRs without MultigresCluster parent.

**Pros**:
- No managed flag complexity
- Simple owner reference model
- GitOps manages all CRs directly

**Cons**:
- Verbose for simple deployments
- No convenience wrapper for common patterns
- Users must manually coordinate component specs

**Rejected**: Parent CR provides important convenience for basic use cases.

## Alternative 2: Immutable `managed` Flag

Only allow setting `managed` at creation time, disallow runtime changes.

**Pros**:
- Simpler - no transition logic needed
- No orphaning complexity
- Clear separation from start

**Cons**:
- Can't migrate existing clusters to GitOps control
- Requires recreating clusters to change management model
- Lost flexibility for production environments

**Rejected**: Runtime transitions are essential for production adoption.

## Alternative 3: Annotation-Based Instead of Spec Field

Use annotation `multigres.com/managed: "false"` instead of spec field.

**Pros**:
- Doesn't pollute API spec
- Can be changed without API versioning

**Cons**:
- Annotations not typed or validated
- Less discoverable for users
- Not visible in `kubectl explain`

**Rejected**: Spec field provides better API clarity and validation.

## Alternative 4: Always Orphan StatefulSets

Automatically orphan all StatefulSets on any deletion.

**Pros**:
- Maximum safety - never delete Pods
- Simpler logic - no conditional orphaning

**Cons**:
- Orphaned StatefulSets accumulate
- Manual cleanup required
- Wastes resources in non-production environments

**Rejected**: Users should have control over cascade vs orphan behavior.

# Infrastructure Needed

- **Kind/k3d clusters**: For local E2E testing
- **Argo CD**: For integration testing (client requirement)
- **Flux**: For basic compatibility validation (lower priority)
- **CI/CD**: GitHub Actions for automated E2E tests
- **Git Repository**: For storing test manifests and GitOps config

# References

- [Kubernetes Owner References](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
- [Kubernetes Garbage Collection](https://kubernetes.io/docs/concepts/architecture/garbage-collection/)
- [Argo CD Resource Exclusions](https://argo-cd.readthedocs.io/en/stable/user-guide/compare-options/)
- [Flux Kustomization](https://fluxcd.io/flux/components/kustomize/kustomization/)

