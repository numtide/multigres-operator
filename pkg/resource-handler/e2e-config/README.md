# E2E Configuration Files

This directory contains sample CRDs for end-to-end testing and local development.

## Files

- **kind-cell.yaml**: Cell resource definition
- **kind-shard.yaml**: Shard resource definition
- **kind-toposerver.yaml**: TopoServer resource definition
- **register-topology.sh**: Script to register topology metadata

## MVP Configuration

The current configurations are set up for the MVP (Minimum Viable Product) with strict requirements:

- Database must be `"postgres"`
- TableGroup must be `"default"`
- Shard name must be `"0-inf"` (unsharded range mode)

Original e2e configurations with custom database/tablegroup names are commented out in the YAML files.

## Topology Registration (Required Manual Step)

**Important**: The `resource-handler` operator only manages Kubernetes resources (Deployments, Services, StatefulSets). It does NOT register cells and databases in the topology server (etcd). This will be implemented in the `data-handler` module.

### Manual Registration for Testing

After deploying the resources, you must manually register topology metadata:

```bash
./register-topology.sh zone-a postgres kind-toposerver-sample default /tmp/kind-kubeconfig.yaml
```

**All 5 arguments are required**:
1. Cells (comma-separated for multiple: `zone-a,zone-b`)
2. Database name
3. TopoServer service name
4. Namespace
5. Kubeconfig path

This script uses the `multigres createclustermetadata` command to properly encode topology data as Protocol Buffers.

### Without Registration

If you skip this step, multipooler will crash with:
```
Error: failed to get existing multipooler: Code: UNAVAILABLE
unable to get connection for cell "zone-a"
```

## Architecture

- **resource-handler**: Manages Kubernetes resources (Deployments, Services, StatefulSets) ✅
- **data-handler**: Will manage topology/data plane operations using multigres CLI/library ⏳
