# Webhook Rejection Scenarios

These are **negative tests**: the mutation MUST be rejected by the admission webhook. The cluster should remain unchanged after each attempt. Always verify the cluster is still Healthy and the observer reports no new findings after the rejection.

---

### storage-shrink-rejection
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects storage size decreases (PVC shrink not supported)

**How to execute:**
1. Record current pool storage size from the MultigresCluster spec.
2. **Mutation**: Attempt to shrink storage size to a smaller value:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"<pool>","storage":{"size":"100Mi"}}]}]}}'
   ```
3. **Expected**: kubectl returns an error containing "storage shrink" or similar rejection.
4. Verify cluster spec is unchanged: `kubectl get multigrescluster <name> -n <ns> -o jsonpath='{.spec.shards[0].pools[0].storage.size}'`
5. Run targeted observer check: `observer '/api/check?categories=crd-status'`

**Success criteria:**
- Webhook rejects the request (HTTP 422 or admission denied)
- Cluster remains Healthy with original storage size
- Observer reports zero new findings

**Teardown:** None needed — mutation was rejected.

### etcd-replicas-immutable
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects etcd replica count changes after creation

**How to execute:**
1. Record current etcd replica count.
2. **Mutation**: Attempt to change etcd replicas:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"topoServer":{"replicas":5}}}'
   ```
3. **Expected**: kubectl returns an error containing "etcd replicas" or "immutable".
4. Verify TopoServer StatefulSet unchanged.

**Success criteria:**
- Webhook rejects the request
- TopoServer StatefulSet replicas unchanged
- Cluster remains Healthy

**Teardown:** None needed.

### invalid-template-reference
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects references to non-existent templates

**How to execute:**
1. **Mutation**: Attempt to set a non-existent template reference:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"coreTemplateRef":"nonexistent-template"}}'
   ```
2. **Expected**: kubectl returns an error containing "template" or "not found".
3. Verify cluster spec unchanged.

**Success criteria:**
- Webhook rejects the request
- Cluster spec retains original template reference
- Cluster remains Healthy

**Teardown:** None needed.

### invalid-pool-name
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects pool names with invalid characters

**How to execute:**
1. **Mutation**: Attempt to add a pool with an invalid name:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"shards":[{"name":"<shard>","pools":[{"name":"INVALID_NAME!","replicasPerCell":1,"cells":["<cell>"]}]}]}}'
   ```
2. **Expected**: kubectl returns a validation error.
3. Verify cluster spec unchanged.

**Success criteria:**
- Webhook or CRD validation rejects the request
- Cluster remains Healthy

**Teardown:** None needed.

### resource-limits-violated
**Tier:** standard | **Fast-path:** yes
**Tests:** Webhook rejects resource configs where limits < requests

**How to execute:**
1. **Mutation**: Attempt to set resource limits below requests:
   ```
   kubectl patch multigrescluster <name> -n <ns> --type merge -p '{"spec":{"databases":[{"name":"postgres","tablegroups":[{"name":"default","shards":[{"name":"<shard>","spec":{"pools":{"<pool>":{"postgres":{"resources":{"requests":{"cpu":"200m"},"limits":{"cpu":"100m"}}}}}}}]}]}]}}'
   ```
2. **Expected**: kubectl returns a validation error containing "limit" and ">= request".
3. Verify cluster spec unchanged.

**Success criteria:**
- Webhook rejects the request with a meaningful error message
- Cluster remains Healthy and unmodified
- Works for cpu and memory, on postgres and multipooler containers

**Teardown:** None needed.
