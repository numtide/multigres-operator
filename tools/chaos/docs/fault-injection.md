# Fault Injection Reference (Level 2)

The fault injector uses [Chaos Mesh](https://chaos-mesh.org/) to simulate infrastructure failures and validates that the Multigres stack recovers correctly. It answers the question: **"Does the cluster survive real infrastructure failures?"**

Deploy with:

```bash
# Install Chaos Mesh first (one-time setup)
kubectl apply -f tools/chaos/deploy/chaos-mesh/

# Deploy the fault injector
CHAOS_LEVEL=2 make kind-deploy-chaos
```

---

## Prerequisites

- **Chaos Mesh** installed in the cluster
- Level 2 RBAC grants access to `chaos-mesh.org` CRDs

---

## Safety Guarantees

Every Chaos Mesh resource created by the tool enforces strict scoping:

1. **Label selector:** All CRDs target only pods with `app.kubernetes.io/managed-by: multigres-operator`
2. **Exact pod selectors:** NetworkChaos uses specific pod names, never namespace-wide selectors
3. **TTL:** All chaos resources have a TTL and are cleaned up in the scenario's teardown phase
4. **Automatic cleanup:** If the chaos pod crashes mid-scenario, the cluster self-heals via the operator. On restart, chaos resources are garbage-collected

---

## Scenario Catalog

### Pod Faults (`fault/pod_fault.go`)

| Scenario | Chaos Mesh Resource | Target | Validation | Timeout |
|----------|-------------------|--------|------------|---------|
| **Kill random pool pod** | PodChaos (pod-kill) | Random pool pod | Operator recreates, re-registers in topo, cluster recovers | 5min |
| **Kill primary pod** | PodChaos (pod-kill) | Primary (from podRoles) | Failover, new primary elected, standby promoted, data intact | 5min |
| **Kill operator pod** | PodChaos (pod-kill) | Operator pod | Deployment recreates, reconciliation resumes, no double-actions | 3min |
| **Kill multipooler container** | PodChaos (container-kill) | multipooler in pool pod | Container restarts, pod stays Running, re-registration | 3min |

### Network Faults (`fault/network_fault.go`)

| Scenario | Chaos Mesh Resource | Effect | Duration | Validation |
|----------|-------------------|--------|----------|------------|
| **Partition pool from etcd** | NetworkChaos (partition) | Pool pod can't reach TopoServer | 30s | Topo operations degrade gracefully, recovery after heal |
| **Partition gateway from pools** | NetworkChaos (partition) | Gateway can't reach multipooler gRPC | 30s | Gateway reports backends unavailable, recovery after heal |
| **Latency on etcd** | NetworkChaos (delay 200ms) | All etcd traffic delayed | 30s | Topo operations slow but succeed, no operator timeouts |
| **Packet loss gateway↔pool** | NetworkChaos (loss 50%) | Half of packets dropped | 30s | Queries may fail, system doesn't crash, recovery |
| **Partition operator from API** | NetworkChaos (partition) | Operator can't reach kube-apiserver | 30s | Operator stops reconciling, resumes after heal, no duplicate actions |

### IO Faults (`fault/io_fault.go`)

| Scenario | Chaos Mesh Resource | Effect | Validation |
|----------|-------------------|--------|------------|
| **Disk latency on pool PVC** | IOChaos (delay 500ms) | Pool pod's data volume has 500ms I/O latency | PostgreSQL slows but doesn't crash |
| **IO errors on etcd data** | IOChaos (errno) | TopoServer PVC returns I/O errors | etcd handles gracefully, recovers after fault |

### Stress Faults (`fault/stress.go`)

| Scenario | Chaos Mesh Resource | Effect | Validation |
|----------|-------------------|--------|------------|
| **CPU stress on pool pod** | StressChaos (cpu) | CPU exhaustion on a pool pod | Queries slow, pod stays Running, no OOMKill |
| **Memory stress on pool pod** | StressChaos (memory) | Memory pressure on a pool pod | If OOMKilled, operator recreates, data intact from PVC |

---

## Scenario Lifecycle

Each fault injection scenario follows the same phases as Level 1, with an additional fault injection step:

```
Setup → Inject Fault → Wait (duration) → Remove Fault → Validate Recovery → Teardown
```

The observer runs continuously during all phases, validating that:
1. **During fault:** The system degrades gracefully (no cascading failures)
2. **After fault:** The system recovers to its pre-fault state
