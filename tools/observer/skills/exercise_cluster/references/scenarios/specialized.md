# Specialized Scenarios

Topology verification, durability policy, log levels, large-scale operations, stateless components, and multi-pool testing.

---

### verify-topology-placement
**Tier:** quick | **Fast-path:** yes
**Tests:** Pods are scheduled on the correct zone-labeled nodes matching their cell's topology
**Requires:** `make kind-deploy-topology` (multi-node kind cluster with zone labels)

**How to execute:**
1. Deploy the `multi-cell-topology` fixture.
2. Wait for CRD phase `Healthy` and all pods Running+Ready.
3. For each cell, verify all its pods run on the node with the matching `topology.kubernetes.io/zone` label:
   ```bash
   for cell in us-east-1a us-east-1b us-east-1c; do
     echo "=== Cell: $cell ==="
     kubectl get pods -n default -l multigres.com/cell=$cell -o wide --no-headers | awk '{print "  " $1 " → node: " $7}'
     echo "  Expected node zone: $cell"
     for node in $(kubectl get pods -n default -l multigres.com/cell=$cell -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | sort -u); do
       actual_zone=$(kubectl get node $node -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}')
       echo "  Node $node has zone: $actual_zone"
       if [ "$actual_zone" != "$cell" ]; then
         echo "  ERROR: Expected zone=$cell but got zone=$actual_zone"
       fi
     done
   done
   ```
4. Verify each component type (pool, multiorch, multigateway) is placed correctly.

**Success criteria:**
- Every pod's node has a `topology.kubernetes.io/zone` label matching the pod's cell
- No pods are Pending (no unschedulable topology constraints)
- Pool pods, multiorch, and multigateway are all distributed across the correct nodes

**Teardown:** Delete the `multi-cell-topology` cluster. This fixture uses `whenDeleted: Delete` so PVCs are cleaned up automatically.

---

### verify-durability-policy
**Tier:** quick | **Fast-path:** yes
**Tests:** DurabilityPolicy from the cluster spec propagates to Shard CRDs
**Applicable fixtures:** `multi-cell-quorum` (has `durabilityPolicy: "MULTI_CELL_AT_LEAST_2"`)

**How to execute:**
1. Deploy the `multi-cell-quorum` fixture.
2. Wait for CRD phase `Healthy` and all pods Running+Ready.
3. Verify the Shard CRD has the correct durabilityPolicy:
   ```bash
   kubectl get shard -n default -o jsonpath='{range .items[*]}{.metadata.name}: durabilityPolicy={.spec.durabilityPolicy}{"\n"}{end}'
   ```
4. Verify the cluster-level durabilityPolicy is set:
   ```bash
   kubectl get multigrescluster multi-cell-quorum -n default -o jsonpath='{.spec.durabilityPolicy}'
   ```
5. Verify the TableGroup CRD also has the durabilityPolicy:
   ```bash
   kubectl get tablegroup -n default -o jsonpath='{range .items[*]}{.metadata.name}: durabilityPolicy={.spec.durabilityPolicy}{"\n"}{end}'
   ```

**Success criteria:**
- Shard CRD `spec.durabilityPolicy` equals `"MULTI_CELL_AT_LEAST_2"`
- TableGroup CRD `spec.durabilityPolicy` equals `"MULTI_CELL_AT_LEAST_2"`
- Cluster remains Healthy

**Teardown:** Delete the `multi-cell-quorum` cluster.

---

### change-log-levels
**Tier:** standard | **Fast-path:** no
**Tests:** Changing log levels triggers rolling update with new `--log-level` flags
**Applicable fixtures:** `log-levels`

**How to patch:**
1. Read the live CR to confirm current logLevels.
2. Patch `spec.logLevels.pgctld` from `"debug"` to `"info"`:
   ```bash
   kubectl patch multigrescluster log-levels --type merge -p '{"spec":{"logLevels":{"pgctld":"info"}}}'
   ```
3. Wait for rolling update to complete (all pods restarted with new args).
4. Verify pgctld containers now have `--log-level=info`.

**Success criteria:**
- All pool pods restarted with updated pgctld `--log-level=info`
- Other log levels unchanged (multipooler still `warn`, etc.)
- Observer reports no errors during or after rolling update

**Teardown:** Patch back to `"debug"`:
```bash
kubectl patch multigrescluster log-levels --type merge -p '{"spec":{"logLevels":{"pgctld":"debug"}}}'
```

---

### large-scale-up-pool
**Tier:** lifecycle | **Fast-path:** no
**Tests:** Large scale-up (4→10) and scale-down (10→4) of pool replicas
**Applicable fixtures:** `minimal-delete`, `minimal-retain`, `multi-pool`

**How to patch:**
1. Read the live CR to find the readWrite pool path and current replicasPerCell.
2. Scale up to 10:
   ```bash
   kubectl patch multigrescluster <name> --type json -p '[{"op":"replace","path":"/spec/databases/0/tablegroups/0/shards/0/spec/pools/main-rw/replicasPerCell","value":10}]'
   ```
3. Wait for all 10 pods Running+Ready (may take 60-120s for parallel creation).
4. Run observer verification — check for split-brain, replication issues.
5. Scale back down to 4:
   ```bash
   kubectl patch multigrescluster <name> --type json -p '[{"op":"replace","path":"/spec/databases/0/tablegroups/0/shards/0/spec/pools/main-rw/replicasPerCell","value":4}]'
   ```
6. Wait for drain state machine to remove 6 pods.
7. Run observer verification again.

**Success criteria:**
- Scale-up: 10 pods Running+Ready within 120s
- Scale-down: 4 pods remaining, 6 pods drained and deleted
- PVCs follow the configured deletion policy (Delete = 4 PVCs, Retain = 10 PVCs)
- Observer `/api/status` shows no error/fatal findings after stabilization

**Teardown:** Ensure replicasPerCell is back to 4.

### large-scale-multigateway
**Tier:** standard | **Fast-path:** yes
**Tests:** MultiGateway deployment scaling 1→5→1
**Also in e2e:** Basic 1→2 scaling in `test/e2e/shared/scaling/`. Exerciser adds data-plane stability verification.
**Applicable fixtures:** `minimal-delete`, `minimal-retain`

**How to patch:**
1. Read the live CR to find the cell spec and current multigateway replicas.
2. Scale up to 5:
   ```bash
   kubectl patch multigrescluster <name> --type json -p '[{"op":"replace","path":"/spec/cells/0/spec/multigateway/replicas","value":5}]'
   ```
3. Wait for 5 multigateway pods Running+Ready.
4. Scale back to 1:
   ```bash
   kubectl patch multigrescluster <name> --type json -p '[{"op":"replace","path":"/spec/cells/0/spec/multigateway/replicas","value":1}]'
   ```

**Success criteria:**
- 5 multigateway pods Running+Ready after scale-up
- 1 multigateway pod after scale-down, 4 terminated cleanly
- Observer reports no errors

**Teardown:** Ensure multigateway replicas is back to 1.

### large-scale-multiadmin
**Tier:** standard | **Fast-path:** yes
**Tests:** MultiAdmin deployment scaling to current+4 and back
**Also in e2e:** Basic 1→2 scaling in `test/e2e/shared/scaling/`. Exerciser adds data-plane stability verification.
**Applicable fixtures:** `multiadmin-lifecycle`, `overrides-complex`

**How to patch:**
1. Read the live CR to find current multiadmin replicas (typically 1 or 2).
2. Scale up by 4 (e.g., 2→6):
   ```bash
   kubectl patch multigrescluster <name> --type merge -p '{"spec":{"multiadmin":{"spec":{"replicas":6}}}}'
   ```
3. Wait for all replicas Running+Ready.
4. Scale back to original:
   ```bash
   kubectl patch multigrescluster <name> --type merge -p '{"spec":{"multiadmin":{"spec":{"replicas":2}}}}'
   ```

**Success criteria:**
- All scaled-up replicas Running+Ready
- Clean scale-down to original count
- Observer reports no errors

**Teardown:** Ensure multiadmin replicas is back to original value.

---

### change-annotations-stateless
**Tier:** standard | **Fast-path:** no
**Tests:** Adding podAnnotations to multiadmin triggers rolling update
**Applicable fixtures:** `minimal-delete`, `minimal-retain`

**How to patch:**
1. Read the live CR to confirm current multiadmin spec.
2. Add a pod annotation:
   ```bash
   kubectl patch multigrescluster <name> --type merge -p '{"spec":{"multiadmin":{"spec":{"podAnnotations":{"test-annotation":"exerciser"}}}}}'
   ```
3. Wait for multiadmin pods to be restarted with the new annotation.
4. Verify annotation is present:
   ```bash
   kubectl get pods -l app.kubernetes.io/component=multiadmin -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations.test-annotation}{"\n"}{end}'
   ```

**Success criteria:**
- All multiadmin pods have `test-annotation: exerciser` annotation
- Observer reports no errors during or after rolling update

**Teardown:** Remove the annotation:
```bash
kubectl patch multigrescluster <name> --type json -p '[{"op":"remove","path":"/spec/multiadmin/spec/podAnnotations"}]'
```

---

### verify-multi-pool
**Tier:** quick | **Fast-path:** yes
**Tests:** Multi-pool setup, PDB-per-pool, independent scaling
**Applicable fixtures:** `multi-pool`

**How to execute:**
1. Deploy the `multi-pool` fixture.
2. Wait for CRD phase `Healthy` and all pods Running+Ready.
3. Verify readWrite pool has 4 pods:
   ```bash
   kubectl get pods -l multigres.com/pool=main-rw --no-headers | wc -l
   ```
4. Verify analytics-ro pool has 3 pods:
   ```bash
   kubectl get pods -l multigres.com/pool=analytics-ro --no-headers | wc -l
   ```
5. Verify 2 PDBs exist (one per pool):
   ```bash
   kubectl get pdb --no-headers | wc -l
   ```
6. Test independent scaling — scale analytics-ro 3→4 while main-rw stays at 4:
   ```bash
   kubectl patch multigrescluster multi-pool --type json -p '[{"op":"replace","path":"/spec/databases/0/tablegroups/0/shards/0/spec/pools/analytics-ro/replicasPerCell","value":4}]'
   ```
7. Verify analytics-ro has 4 pods, main-rw still has 4 pods.
8. Scale analytics-ro back to 3:
   ```bash
   kubectl patch multigrescluster multi-pool --type json -p '[{"op":"replace","path":"/spec/databases/0/tablegroups/0/shards/0/spec/pools/analytics-ro/replicasPerCell","value":3}]'
   ```

**Success criteria:**
- main-rw pool: 4 pods Running+Ready
- analytics-ro pool: 3 pods Running+Ready (then 4, then back to 3)
- 2 PDBs present (one per pool)
- Pools scale independently without affecting each other
- Observer reports no errors

**Teardown:** Delete the `multi-pool` cluster.

---

### verify-external-adminweb
**Tier:** quick | **Fast-path:** yes
**Tests:** ExternalAdminWeb config lifecycle — Service externalIPs, annotations, status, and conditions
**Applicable fixtures:** `external-adminweb`

**How to execute:**
1. Deploy the `external-adminweb` fixture.
2. Wait for CRD phase `Healthy` and all pods Running+Ready.
3. Verify the multiadmin-web Service has externalIPs and annotations:
   ```bash
   kubectl get svc ext-adminweb-multiadmin-web -o jsonpath='{.spec.externalIPs}'
   # Expected: ["198.51.100.10"]
   kubectl get svc ext-adminweb-multiadmin-web -o jsonpath='{.metadata.annotations.service\.beta\.kubernetes\.io/aws-load-balancer-scheme}'
   # Expected: internet-facing
   ```
4. Verify status reports the external endpoint:
   ```bash
   kubectl get multigrescluster ext-adminweb -o jsonpath='{.status.adminWeb.externalEndpoint}'
   # Expected: 198.51.100.10
   ```
5. Verify the AdminWebExternalReady condition:
   ```bash
   kubectl get multigrescluster ext-adminweb -o jsonpath='{.status.conditions[?(@.type=="AdminWebExternalReady")].status}'
   # Expected: True (once multiadmin-web Deployment has ready replicas)
   kubectl get multigrescluster ext-adminweb -o jsonpath='{.status.conditions[?(@.type=="AdminWebExternalReady")].reason}'
   # Expected: EndpointReady
   ```

**Success criteria:**
- Service has `spec.externalIPs` matching the CR
- Service has user-provided annotations
- `status.adminWeb.externalEndpoint` populated
- `AdminWebExternalReady` condition is `True` with reason `EndpointReady`

---

### external-adminweb-enable-disable
**Tier:** quick | **Fast-path:** no
**Tests:** Toggling externalAdminWeb between enabled and disabled
**Applicable fixtures:** `external-adminweb`

**How to patch (disable):**
```bash
kubectl patch multigrescluster ext-adminweb --type merge -p '{"spec":{"externalAdminWeb":{"enabled":false}}}'
```

**What to observe after disabling:**
- Service reverts to plain ClusterIP (no externalIPs, no custom annotations)
- `status.adminWeb` becomes null
- `AdminWebExternalReady` condition is removed

**How to patch (re-enable with different IPs):**
```bash
kubectl patch multigrescluster ext-adminweb --type merge -p '{"spec":{"externalAdminWeb":{"enabled":true,"externalIPs":["203.0.113.50"],"annotations":{"new-annotation":"re-enabled"}}}}'
```

**What to observe after re-enabling:**
- Service updated with new externalIP `203.0.113.50`
- Service has new annotation
- `status.adminWeb.externalEndpoint` set to `203.0.113.50`
- `AdminWebExternalReady` condition returns with appropriate status

**Teardown:** Patch back to original state or delete cluster.

---

### external-adminweb-change-annotations
**Tier:** quick | **Fast-path:** yes
**Tests:** Changing annotations on externalAdminWeb propagates to Service
**Applicable fixtures:** `external-adminweb`

**How to patch:**
```bash
kubectl patch multigrescluster ext-adminweb --type merge -p '{"spec":{"externalAdminWeb":{"enabled":true,"externalIPs":["198.51.100.10"],"annotations":{"service.beta.kubernetes.io/aws-load-balancer-scheme":"internal","new-key":"new-value"}}}}'
```

**What to observe:**
- Service annotations updated (scheme changed, new key added)
- Old annotations removed via SSA field ownership
- `status.adminWeb` and condition unchanged (externalIPs didn't change)

**Teardown:** Patch back to original annotations.
