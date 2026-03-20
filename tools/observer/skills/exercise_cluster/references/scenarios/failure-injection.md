# Failure Injection Scenarios

Scenarios that deliberately break components to verify the operator and observer detect and recover from failures.

---

### etcd-unavailability
**Tier:** standard | **Fast-path:** no
**Tests:** Behavior when the topology store (etcd) becomes unavailable

> **Known limitation:** Scaling the TopoServer StatefulSet to 0 replicas does NOT work — the operator immediately reconciles it back to the desired replica count (self-healing). You must use alternative injection methods.

**How to execute:**
1. Record baseline TopoServer StatefulSet state.
2. **Mutation** — use ONE of these approaches (in order of preference):
   - **NetworkPolicy injection** (recommended): Create a NetworkPolicy that blocks traffic to the etcd pods on port 2379:
     ```bash
     kubectl apply -f - <<'EOF'
     apiVersion: networking.k8s.io/v1
     kind: NetworkPolicy
     metadata:
       name: block-etcd
       namespace: <ns>
     spec:
       podSelector:
         matchLabels:
           app.kubernetes.io/component: topo-server
       policyTypes: [Ingress]
       ingress: []  # deny all ingress
     EOF
     ```
   - **Fast pod deletion loop**: Delete etcd pods in a tight loop faster than the StatefulSet controller can recreate them (less reliable):
     ```bash
     for i in $(seq 1 10); do kubectl delete pod -n <ns> -l app.kubernetes.io/component=topo-server --force --grace-period=0; sleep 2; done
     ```
   - **Do NOT use `kubectl scale`** — the operator reconciles it back immediately.
3. Wait 60s for the system to detect the failure.
4. **Observe**:
   - Observer topology check should report errors: `observer '/api/check?categories=topology'`
   - Shard/cluster phase should transition to Degraded
   - Data plane (pool pods) should continue serving reads
5. **Recovery**: Remove the NetworkPolicy (or stop the deletion loop):
   ```bash
   kubectl delete networkpolicy block-etcd -n <ns>
   ```
6. Run full Stability Verification Protocol.

**Success criteria:**
- Observer detects topology unavailability within 1-2 cycles
- Data plane continues serving read queries during etcd outage
- After recovery, topology is re-established
- Cluster returns to Healthy phase
- Observer history shows the topology errors as transient (not persistent)

**What to observe:**
- How long until the operator detects etcd is down
- Whether multiorch continues functioning with cached state
- Whether any pool pods restart unnecessarily
- Recovery time from etcd restore to Healthy phase

**Teardown:** Delete the NetworkPolicy if not done in step 5.

### image-pull-backoff
**Tier:** standard | **Fast-path:** no
**Tests:** Behavior when a pool pod has an invalid image (simulates registry failure)

> **Known limitation:** Pool pod images cannot be injected via CRD patching. The `ContainerConfig` type only has a `resources` field — there is no `image` field on `PoolSpec`. Images are set at cluster level via `spec.images.postgres` (or compiled defaults in `api/v1alpha1/image_defaults.go`). Changing `spec.images.postgres` affects ALL pool pods cluster-wide, not a single pool.

**How to execute:**
1. Record current postgres image:
   ```bash
   kubectl get pods -n <ns> -l app.kubernetes.io/component=shard-pool -o jsonpath='{.items[0].spec.containers[?(@.name=="postgres")].image}'
   ```
2. **Mutation** — use ONE of these approaches:
   - **Cluster-wide image patch** (affects all pool pods):
     ```bash
     kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"images":{"postgres":"invalid-image:nonexistent"}}}'
     ```
   - **Direct pod image override** (affects a single pod, bypasses operator):
     ```bash
     kubectl set image -n <ns> pod/<pool-pod-name> postgres=invalid-image:nonexistent
     ```
     Note: The operator will eventually reconcile this back, so observe quickly.
3. Wait 60-90s for the rolling update to start and the new pod to fail.
4. **Observe**:
   - New pod should enter `ImagePullBackOff` or `ErrImagePull` state
   - Observer pod-health check should detect the failing pod: `observer '/api/check?categories=pod-health'`
   - Shard phase should transition to Degraded
5. **Recovery**: Restore the valid image:
   ```bash
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"images":{"postgres":"<original-image>"}}}'
   ```
6. Run full Stability Verification Protocol.

**Success criteria:**
- Observer detects ImagePullBackOff via pod-health check
- Shard phase transitions to Degraded
- After image restoration, pod recovers and shard returns to Healthy
- Observer history shows the pod-health findings as transient
- No data loss or replication breaks during the incident

**What to observe:**
- Whether the operator's rolling update blocks (doesn't replace more pods while one is failing)
- Whether the drain state machine is involved or bypassed
- Recovery time from image restore to Healthy

**Teardown:** Restore original image if not done in step 5.
