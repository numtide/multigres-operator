# Concurrent Mutation Scenarios

These scenarios fire two mutations in rapid succession to test race conditions in the operator's reconciliation loop. Only run in **full** execution mode. Use the **Concurrent Mutation Protocol** from `references/concurrent-mutations.md`.

**Applicable fixtures:** `minimal-retain`, `minimal-delete`, `templated-full`.

---

### concurrent-scale-and-delete-pod
**Tier:** standard | **Fast-path:** no
**Tests:** Overlapping scale-up reconciliation with pod replacement

**How to execute:**
1. Record baseline: pool pod count, current replicasPerCell.
2. **Mutation A**: Scale pool replicas +1 (JSON patch replicasPerCell).
3. **Mutation B** (within 2-3 seconds): Delete a pool pod (`kubectl delete pod <name>`).
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Total pool pod count == original + 1 (scale succeeded)
- Deleted pod has been replaced (no missing pods)
- All pods Running+Ready
- Observer connectivity and replication healthy

**What to observe:**
- The operator must handle the scale-up and the pod deletion without interfering
- Watch for pods stuck in Pending or pods being double-created
- Replication should include all pods after stabilization

**Teardown:** Scale replicasPerCell back to original value.

### concurrent-config-and-scale
**Tier:** standard | **Fast-path:** no
**Tests:** Overlapping resource update (rolling restart) with gateway scaling

**How to execute:**
1. Record baseline: pool pod resources, gateway replicas.
2. **Mutation A**: Update pool postgres resources (JSON patch).
3. **Mutation B** (within 2-3 seconds): Scale multigateway replicas +1 (merge patch).
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Pool pods have updated resource values
- Gateway Deployment readyReplicas == new target
- All pods Running+Ready

**What to observe:**
- Rolling restart from resource change should not block gateway scale-up
- Both controllers (resource-handler and cell controller) reconcile without conflict
- No resource version conflicts causing retry storms

**Teardown:** Restore original resources and gateway replicas.

### concurrent-template-and-pod-delete
**Tier:** standard | **Fast-path:** no
**Applicable:** Template-based fixtures only
**Tests:** Template CR mutation during pod replacement

**How to execute:**
1. Record baseline: template values, pool pod count.
2. **Mutation A**: Patch the ShardTemplate CR (e.g., change multiorch resources).
3. **Mutation B** (within 2-3 seconds): Delete a pool pod.
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Template change propagated to multiorch pods
- Deleted pool pod replaced
- All pods Running+Ready

**What to observe:**
- Template reconciliation and pod replacement must not interfere
- Watch for template values not propagating because the controller was busy with pod replacement
- Check observer history for flapping findings

**Teardown:** Restore template to original values.

### concurrent-dual-scale
**Tier:** standard | **Fast-path:** no
**Tests:** Two independent scale operations at the same time

**How to execute:**
1. Record baseline: pool replicasPerCell, gateway replicas.
2. **Mutation A**: Scale pool replicas +1.
3. **Mutation B** (within 2-3 seconds): Scale multigateway replicas +1.
4. Run full Stability Verification Protocol.

**Combined success criteria:**
- Pool pod count per cell == new replicasPerCell
- Gateway Deployment readyReplicas == new target
- All pods Running+Ready

**What to observe:**
- Both scale operations should succeed independently
- Resource version conflicts should be retried cleanly by the operator
- No pods stuck in Pending or ContainerCreating

**Teardown:** Restore both replica counts to original values.
