# E2E Testing with Kind

**Status:** ✅ Completed

> [!NOTE]
> **This proposal has been implemented.** The kind-based E2E infrastructure is in place under `test/e2e/`
> with `pkg/testutil/kind.go` providing the `SetUpKind` and `SetUpKindManager` utilities as described.
> The Makefile includes a `test-e2e` target. The tests exercise full operator lifecycle including
> resource creation/deletion with real Kubernetes GC.

## Overview
Add e2e testing infrastructure that uses kind (real Kubernetes cluster) instead
of envtest, following the same testutil patterns as integration tests.

## Why kind over envtest for e2e?
- envtest only runs kube-apiserver + etcd (no garbage collection, no schedulers)
- kind runs a full Kubernetes cluster (owner ref cascading deletion works, pods actually schedule)
- Tests the operator as it would run in production

## Architecture

### Agent A: pkg/testutil/kind.go - Kind test utilities
Add to the existing testutil module:
- `SetUpKind(t, opts...)` - connects to existing kind cluster, installs CRDs, returns `*rest.Config`
- `SetUpKindManager(t, scheme, opts...)` - convenience combining SetUpKind + SetUpManager + StartManager
- Options: `WithKindCluster(name)`, `WithCRDPaths(paths...)`
- Uses environment variables KIND_CLUSTER (default: "multigres-operator-test-e2e") and KIND (default: "kind")
- Installs CRDs via kubectl apply (real cluster, not envtest CRD loading)

### Agent B: test/e2e/ - E2E test files
Create test directory with:
- `e2e_test.go` - build tag `e2e`, tests that exercise the full operator
- Test: Create a MultigresCluster CR → verify child resources (Cells, Shards, TopoServers) are created
- Test: Delete a MultigresCluster CR → verify cascading deletion works (the thing envtest can't do)
- Update Makefile test-e2e target to use standard testing instead of Ginkgo

## File Scopes
- Agent A: `pkg/testutil/kind.go`, `pkg/testutil/kind_test.go`
- Agent B: `test/e2e/`, `Makefile` (test-e2e target only)
