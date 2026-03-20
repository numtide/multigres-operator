# Observer Query Catalog

All queries use the `observer()` helper function defined in the main skill. Define it if not already set:
```bash
observer() {
  KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n multigres-operator deploy/multigres-observer -- curl -sf "http://localhost:9090$1"
}
```

## Status Queries

```bash
# Just the summary
observer /api/status | jq '.summary'

# Unhealthy checks only
observer /api/status | jq '.healthy | to_entries | map(select(.value == false))'

# Fatal and error findings only
observer /api/status | jq '[.findings[] | select(.level == "fatal" or .level == "error")]'

# Findings for a specific check
observer /api/status | jq '[.findings[] | select(.check == "replication")]'
```

## Connectivity Probes

```bash
# All failed probes
observer /api/status | jq '[.probes.connectivity[] | select(.ok == false)]'

# gRPC health probes (detect pool starvation / hanging multipooler)
observer /api/status | jq '[.probes.connectivity[] | select(.check == "multipooler-grpc-health")]'

# Multiorch pooler health (detect when orchestrator sees all poolers down)
observer /api/status | jq '[.probes.connectivity[] | select(.check == "multiorch-pooler-health")]'
```

## Readiness and Replication

```bash
# Readiness cross-check failures (Kubernetes says Ready but observer probes disagree)
observer /api/status | jq '[.findings[] | select(.message | test("reports Ready but"))]'

# Replication topology (per-shard primary/replica counts)
observer /api/status | jq '.probes.replication'
```

## Inventory and State

```bash
# Pod inventory
observer /api/status | jq '.probes.pods'

# Drain state overview
observer /api/status | jq '.probes.drain'

# CRD status overview
observer /api/status | jq '.probes.crdStatus'
```

## On-Demand Targeted Checks

Use `/api/check` to run specific checks immediately without waiting for the next cycle:
```bash
# Run only replication and connectivity checks
observer '/api/check?categories=replication,connectivity' | jq .

# Run all checks on demand
observer /api/check | jq .
```

Valid categories: `pod-health`, `resource-validation`, `crd-status`, `drain-state`, `connectivity`, `logs`, `events`, `topology`, `replication`

## Finding History

```bash
# Full history with classification
observer /api/history | jq .

# Just persistent and flapping findings (the ones that matter)
observer /api/history | jq '{
  persistent: [.persistent[]? | {check, component, message, severity, count}],
  flapping: [.flapping[]? | {check, component, message, severity, count}],
  transientCount: (.transient | length),
  totalCycles: .totalCycles,
  observerStartedAt: .observerStartedAt
}'
```

**`observerStartedAt`** tells you when the observer process started. If `totalCycles` is low and `observerStartedAt` is recent, findings may not have stabilized yet — wait for more cycles before classifying.

## Fallback: Read Logs Directly

If `kubectl exec` into the observer pod isn't available, fall back to logs. Observer logs use double-nested JSON:
```bash
KUBECONFIG=kubeconfig.yaml kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator --tail=200 | grep '\"fatal\"'
KUBECONFIG=kubeconfig.yaml kubectl logs -l app.kubernetes.io/name=multigres-observer -n multigres-operator --tail=200 | grep '\"error\"'
```
