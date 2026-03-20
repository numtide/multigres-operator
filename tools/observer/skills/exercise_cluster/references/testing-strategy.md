# Testing Strategy: E2E vs Exerciser

The operator has two complementary testing layers:

**E2E tests** (`test/e2e/`) run in CI on every PR. They use a Kind cluster with the operator deployed but **no real multigres data plane**. They verify pure operator behavior:
- Resource creation (PDBs, Deployments, Services)
- Config propagation (log levels, template values)
- Stateless component scaling (multiadmin, multigateway, etcd)
- Webhook/CEL validation (append-only cells, append-only pools)
- Cluster deletion and cleanup

**Exerciser scenarios** (this skill) run manually via an agent on a Kind or EKS cluster with **real multigres running**. They verify data-plane correctness during operator mutations:
- Pool replica scaling (drain state machine, replication setup)
- Rolling updates (connection continuity, zero-downtime)
- Concurrent mutations (race conditions between reconciliations)
- Observer-verified health (connectivity, replication, split-brain detection)

**Rule of thumb:** If a test only needs kubectl + the operator, it belongs in e2e. If it needs the observer or a working multigres data plane to verify correctness, it belongs here.

Scenarios marked with **"Also in e2e"** have basic operator-level coverage in CI. The exerciser version adds observer-based data-plane verification that cannot run without real multigres.
