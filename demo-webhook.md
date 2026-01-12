# Multigres Operator: Hybrid Admission Control Demo

This document demonstrates the **Hybrid Architecture** of the Multigres Operator. It highlights how the operator balances user experience, safety, and operational resiliency through a design that utilizes both a Kubernetes Webhook and an internal logic resolver.

## Overview: Two Modes of Operation

The Multigres Operator implements a Shared Resolver pattern. This logic is shared between the API Server (via Webhook) and the Controller (via Reconciliation Loop), providing two distinct modes:

1. **Robust Mode (Normal Operation)**: A Mutating Webhook intercepts requests to "hydrate" configurations before they are stored in etcd. This ensures users see the full, realized state of their cluster ("Visible Defaults") and protects critical resources via Validating Webhooks. Certificates are self-signed unless users mounts their own.

2. **Development Mode (No Webhook)**: If the webhook service is unavailable or disabled, the Controller falls back to applying defaults in-memory. This ensures the operator remains functional and resilient ("Resilient Fallback"), even if admission control is bypassed.

## 🛠️ Prerequisites

The Operator's `Makefile` handles most project-specific dependencies (Kustomize, Controller-Gen), but you must have the base runtime installed on your machine.

**System Requirements:**

1. **Go (1.23+)**: Required to compile the operator.
2. **Docker (or Podman)**: Required to build container images.
3. **Kind**: Required to create the local Kubernetes cluster.
4. **Kubectl**: Required to interact with the cluster.
5. **Make**: Required to run the build scripts.

*Note: The demo commands below will fail immediately if these tools are not found in your `$PATH`.*

---

## 🏗️ Scenario 1: Robust Mode (Visible Defaults)

**Objective:** Demonstrate the "Zero Config" experience where the operator sets up its own secure webhook infrastructure and hydrates configurations.

### 1. Deploy the Operator

We deploy the operator using the standard target. This builds the image, loads it into Kind, and applies the manifests.

```bash
make kind-deploy
kubectl wait --for=condition=Available deployment/multigres-operator-controller-manager -n multigres-operator --timeout=120s

```

### 2. Verify Internal Cert Mode

The operator automatically detects that no certificates were provided and enters **"Self-Bootstrap"** mode.

```bash
kubectl logs -n multigres-operator deployment/multigres-operator-controller-manager | grep "webhook certificates"

```

**Output:**
`webhook certificates not found on disk; enabling internal certificate rotation`

### 3. Apply a Minimal Configuration

We apply a cluster definition consisting of only a single Cell.

```bash
kubectl apply -f config/samples/minimal.yaml

```

### 4. Inspect the Result (Visible Defaults)

The Mutating Webhook intercepts the request and uses the Shared Resolver to populate the spec *before* it is persisted.

```bash
kubectl get multigrescluster minimal-cluster -o yaml

```

**What to look for:**

* **Images:** Pinned versions (`postgres:15-alpine`, etc.) are injected.
* **System Catalog:** The default `postgres` database and tablegroups are created.
* **Resources:** CPU/Memory requests are fully materialized.

---

## 🚀 Scenario 2: Advanced Configuration & Safety Gates

**Objective:** Demonstrate Template Resolution and the **"Fast Fail"** protection provided by the Validating Webhook.

### 1. Install Standard Templates

We install reusable Templates defining a high-availability topology.

```bash
kubectl apply -f config/samples/templates/

```

### 2. Test: Missing Template (Webhook Denial)

Before creating the cluster, we demonstrate the validation logic by ensuring the webhook rejects invalid references.

**Action:** Delete a required template (`standard-core`) and try to apply the cluster configuration.

```bash
kubectl delete coretemplate standard-core
kubectl apply -f config/samples/templated-cluster.yaml

```

**Output:**
The Webhook immediately rejects the request with a clear error.

```text
Error from server (NotFound): error when creating "config/samples/templated-cluster.yaml": 
admission webhook "mmultigrescluster.kb.io" denied the request: CoreTemplate.multigres.com "standard-core" not found

```

### 3. Apply a Templated Cluster (Success)

We restore the missing template and apply the cluster again. This time it succeeds.

```bash
kubectl apply -f config/samples/templates/core.yaml
kubectl apply -f config/samples/templated-cluster.yaml

```

### 4. Verify Resolution

Check that the cluster inherited values from the templates.

```bash
kubectl get multigrescluster standard-ha-cluster -o yaml

```

**Output:**
`replicas: 2` is applied, overriding the default of 1.

### 5. Test: Safety Gate (Referential Integrity)

Now that the cluster exists and relies on the templates, we verify that the webhook prevents accidental deletion.

```bash
kubectl delete celltemplate standard-cell

```

**Output:**
The webhook blocks the deletion.

```text
Error from server (Forbidden): admission webhook "vcelltemplate.kb.io" denied the request: 
cannot delete CellTemplate 'standard-cell' because it is in use by MultigresCluster 'standard-ha-cluster'

```

---

## 🏭 Scenario 3: Production Mode (Cert-Manager Integration)

**Objective:** Demonstrate how to use **Cert-Manager** for certificate management.

### 1. Architecture

To switch to External Mode using Cert-Manager, we utilize a Kustomize overlay (`config/deploy-certmanager`).

* **Mechanism:** This creates a `Certificate` resource. Cert-Manager creates a Secret. The Deployment mounts that Secret. The Operator detects the files on disk and automatically disables its internal CA.

### 2. Deploy with Cert-Manager

We use a convenience make target that installs Cert-Manager and deploys the operator with the overlay enabled.

```bash
make kind-down # Clean slate
make kind-deploy-certmanager
kubectl wait --for=condition=Available deployment/multigres-operator-controller-manager -n multigres-operator --timeout=180s

```

### 3. Verify External Mode

Check the logs to confirm the operator detected the certificates provided by Cert-Manager.

```bash
kubectl logs -n multigres-operator deployment/multigres-operator-controller-manager | grep "webhook certificates"

```

**Output:**
`webhook certificates found on disk; using external certificate management`

### 4. Functional Test

Apply a cluster to prove the webhook is still working correctly using the external certificates.

```bash
kubectl apply -f config/samples/minimal.yaml
kubectl get multigrescluster minimal-cluster -o yaml

```

**Output:**
The spec is **Fully Hydrated** (Visible Defaults), proving the API Server trusts our Cert-Manager certificate.

---

## 🛡️ Scenario 4: Development Mode (Resiliency Fallback)

**Objective:** Demonstrate operational continuity when the Webhook is disabled. Unlike "Strict" operators that fail completely, Multigres falls back to "Invisible Defaults," but loses the "Fast Fail" protections.

### 1. Deploy without Webhook

We deploy the operator with the webhook explicitly disabled.

```bash
make kind-down
make kind-deploy-no-webhook

```

### 2. Apply the Minimal Cluster

We apply the exact same minimal YAML from Scenario 1.

```bash
kubectl apply -f config/samples/minimal.yaml

```

### 3. Inspect the Object (Invisible Defaults)

Because the webhook is bypassed, the object is stored "as-is" in `etcd`.

```bash
kubectl get multigrescluster minimal-cluster -o yaml

```

**Output:**
The spec is **Sparse**. No images or resources are visible.

### 4. The Resiliency Payoff

Despite the sparse spec, the **Controller** successfully creates the child resources using in-memory defaults.

```bash
kubectl get cells,tablegroups -l multigres.com/cluster=minimal-cluster

```

**Output:**
The child resources are created successfully.

```text
NAME                                        GATEWAY   READY
cell.multigres.com/minimal-cluster-zone-a   0         

NAME                                                        SHARDS
tablegroup.multigres.com/minimal-cluster-postgres-default   0

```

### 5. Test: Missing Template (No Immediate Denial)

We repeat the test from Scenario 2. We try to create a cluster referencing a missing template. Since the webhook is disabled, the API server accepts the invalid request.

```bash
kubectl delete coretemplate --all
kubectl apply -f config/samples/templated-cluster.yaml

```

**Output:**
The request is **Accepted** (Created). The user sees no error on the CLI because the admission webhook is inactive.

```text
multigrescluster.multigres.com/standard-ha-cluster created

```

### 6. Inspecting the Failure (Events)

Since the invalid object made it to the Controller, the Controller attempts to reconcile it but fails. The user must check Events to diagnose the issue.

```bash
kubectl describe multigrescluster standard-ha-cluster

```

**Output (Events):**

```text
Events:
  Type     Reason           Age           From                         Message
  ----     ------           ---           ----                         -------
  Warning  TemplateMissing  2s (x10 over 4s)  multigrescluster-controller  referenced CoreTemplate 'standard-core' not found: CoreTemplate.multigres.com "standard-core" not found

```

---

## Clean-up

```bash
make kind-down
```


## 🧠 Internal Architecture

How does the operator achieve this hybrid behavior?

### 1. The Shared Resolver

Logic is centralized in `pkg/resolver`. Both the Webhook (for mutation) and the Controller (for reconciliation) call this same code.

```go
// pkg/resolver/resolver.go
func (r *Resolver) Resolve(ctx context.Context, cluster *multigresv1alpha1.MultigresCluster) (*multigresv1alpha1.MultigresCluster, error) {
    // 1. Load Templates
    // 2. Merge Defaults (Images, Resources)
    // 3. Return fully hydrated struct
    // This function is pure logic; it doesn't care if it's called by Webhook or Controller.
}

```

### 2. Parent-Child Relationship

The Controller implements a hierarchy. The `MultigresCluster` is the parent; it owns `Cells`, `TableGroups`, and `TopoServers`.

```go
// pkg/cluster-handler/controller/multigrescluster/multigrescluster_controller.go
func (r *MultigresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // ...
    // 1. Resolve State (using Shared Resolver)
    resolvedCluster, err := r.Resolver.Resolve(ctx, &cluster)
    
    // 2. Reconcile Children based on Resolved State
    if err := r.reconcileCells(ctx, resolvedCluster); err != nil { return ... }
    if err := r.reconcileDatabase(ctx, resolvedCluster); err != nil { return ... }
    
    return ctrl.Result{}, nil
}

```

### 3. Certificate Self-Bootstrap

The `pkg/webhook/cert` package handles the "Internal Mode." It uses a Kubernetes Secret as a coordination point.

```go
// pkg/webhook/cert/manager.go
func (m *CertRotator) ensureCA(ctx context.Context) (*CAArtifacts, error) {
    // 1. Check if "multigres-webhook-certs" Secret exists
    // 2. If not, Generate RSA Key + X509 Cert
    // 3. Save to Secret
    // 4. Patch MutatingWebhookConfiguration with CA Bundle
}

```