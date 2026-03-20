# Code Investigation Guide

Once you have findings and log evidence, investigate whether the bug is in the **operator** or **upstream multigres**.

## Determine Which Multigres Version is Running

Check what image tags the running pods are using:
```bash
KUBECONFIG=kubeconfig.yaml kubectl get pods -A \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{" "}{end}{"\n"}{end}' \
  | grep multigres
```

Or check the defaults compiled into the operator:
```bash
cat api/v1alpha1/image_defaults.go
```

Image tags use the format `sha-XXXXXXX` which corresponds to a commit SHA in the multigres repo.

## Investigate Operator Code

Search the operator repo for code related to the finding:

| Finding Category | Where to Look |
|---|---|
| Replication | `pkg/resource-handler/controller/shard/` — especially `containers.go` (pod names), `pool_pod.go` (pod spec construction) |
| Drain | `pkg/data-handler/drain/drain.go` — the drain state machine |
| Status | `pkg/resource-handler/controller/shard/shard_controller.go` — status reconciliation |
| Connectivity/resources | `pkg/resource-handler/controller/` for the relevant component |
| Topology | `pkg/data-handler/topo/` — etcd topology operations |

Also see `exercise_cluster/references/operator-knowledge.md` for a more detailed mapping of observer checks to operator code paths.

## Investigate Upstream Multigres Code

Clone or pull the latest upstream code:
```bash
if [ -d /tmp/multigres ]; then
  cd /tmp/multigres && git fetch origin && git pull origin main
else
  git clone https://github.com/multigres/multigres /tmp/multigres
fi
```

**Check the version actually running, not just latest.** If the bug exists on `main` but not on the SHA the operator is using (or vice versa), note that in the report:
```bash
cd /tmp/multigres
git checkout <sha-from-image-tag>
```

Key upstream directories:

| Directory | Responsibility |
|---|---|
| `go/cmd/multigateway/` | Gateway behavior, routing, ports |
| `go/cmd/multipooler/` | Connection pooling, health endpoints |
| `go/cmd/multiorch/` | Orchestration, failover, replication management |
| `go/cmd/pgctld/` | PostgreSQL lifecycle, startup, shutdown |
| `go/proto/` | gRPC service definitions |
