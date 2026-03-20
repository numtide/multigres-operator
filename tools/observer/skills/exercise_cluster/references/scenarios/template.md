# Template & Override Scenarios

These scenarios test the template resolution and override merging system. Applicable to fixtures that use `templateDefaults` and/or inline overrides (`templated-full`, `overrides-complex`).

---

### update-core-template-cr
**Tier:** standard
**Applicable:** Template-based fixtures with CoreTemplate
**Tests:** Mutating a CoreTemplate CR propagates changes to all derived resources

**How to execute:**
1. Read current CoreTemplate CR: `kubectl get coretemplate <name> -n <ns> -o yaml`
2. Patch multiadmin replicas:
   ```bash
   kubectl patch coretemplate <name> -n <ns> --type=merge -p '{"spec":{"multiadmin":{"replicas":3}}}'
   ```
3. Wait for reconciliation (watch multiadmin Deployment).
4. Verify multiadmin Deployment now has 3 replicas.
5. Verify etcd StatefulSet is unchanged (wasn't patched).

**What to observe:**
- Controller detects template change and re-reconciles
- Multiadmin Deployment scales to match new template value
- Other CoreTemplate-derived resources (etcd) remain untouched
- If multiadmin does NOT update, the controller is not watching template changes

**Teardown:** Patch CoreTemplate back to original replicas.

### update-cell-template-cr
**Tier:** standard
**Applicable:** Template-based fixtures with CellTemplate
**Tests:** Mutating a CellTemplate CR propagates to all gateway deployments

**How to execute:**
1. Read current CellTemplate CR.
2. Patch multigateway replicas:
   ```bash
   kubectl patch celltemplate <name> -n <ns> --type=merge -p '{"spec":{"multigateway":{"replicas":3}}}'
   ```
3. Wait for reconciliation.
4. Verify all gateway Deployments update to 3 replicas.

**What to observe:**
- All cells referencing this template update their gateway replica count
- Observer connectivity checks cover any new gateway pods

**Teardown:** Patch CellTemplate back to original replicas.

### update-shard-template-pool
**Tier:** standard
**Applicable:** Template-based fixtures with ShardTemplate
**Tests:** Mutating ShardTemplate pool config triggers pod/PVC changes

**How to execute:**
1. Read current ShardTemplate CR.
2. Patch pool resources:
   ```bash
   kubectl patch shardtemplate <name> -n <ns> --type=merge -p '{"spec":{"pools":{"read-write":{"postgres":{"resources":{"requests":{"cpu":"200m","memory":"256Mi"}}}}}}}'
   ```
3. Wait for reconciliation and rolling restart.

**What to observe:**
- Pool pods restart with new resource limits (rolling update)
- Brief connectivity errors during restart are expected
- After restart, all pods have the new resources
- Replication re-establishes fully

**Teardown:** Patch ShardTemplate back to original resources.

### override-wins-over-template
**Tier:** standard
**Applicable:** `overrides-complex` (requires both template and override for the same field)
**Tests:** Explicit override takes precedence when template is mutated to conflict

**How to execute:**
1. Confirm the cluster has an inline override for multiadmin replicas (e.g., replicas=2).
2. Patch the CoreTemplate to set a DIFFERENT multiadmin replicas value:
   ```bash
   kubectl patch coretemplate <name> -n <ns> --type=merge -p '{"spec":{"multiadmin":{"replicas":5}}}'
   ```
3. Wait for reconciliation.
4. Verify multiadmin Deployment stays at 2 (the override value), NOT 5 (the template value).

**What to observe:**
- Override precedence is preserved: inline spec > template
- If multiadmin changes to 5, the merge logic is broken (template incorrectly winning)
- If multiadmin stays at 2, the precedence chain is correct

**Teardown:** Patch CoreTemplate back to original value.

### template-pod-labels-merge
**Tier:** quick
**Applicable:** Fixtures with podLabels at template and override levels
**Tests:** PodLabels from template and override are merged additively, not replaced

**How to execute:**
1. Verify the ShardTemplate has `podLabels` set (e.g., `{"source": "shard-template"}`).
2. Verify the cluster's shard override has different `podLabels` (e.g., `{"from": "override"}`).
3. Check multiorch pods for both labels:
   ```bash
   kubectl get pods -n <ns> -l app.kubernetes.io/component=multiorch -o jsonpath='{range .items[0]}{.metadata.labels}{"\n"}{end}'
   ```
4. Verify both `source=shard-template` AND `from=override` are present.

**What to observe:**
- Labels are merged (additive) — both template and override labels appear
- If only override labels appear, the template labels were clobbered (merge bug)
- If only template labels appear, the override wasn't applied
- Standard Kubernetes labels (`app.kubernetes.io/*`) should always be present regardless

**Teardown:** Not needed (read-only verification).

---

### switch-template
**Tier:** lifecycle
**Applicable:** Template-based fixtures (`templated-full`)

**How to patch:**
1. Read current `.spec.templateDefaults.cellTemplate`.
2. Create a new CellTemplate CR with slightly different config (e.g., different gateway replicas).
3. Merge patch `templateDefaults.cellTemplate` to point to the new template.

**What to observe:**
- Template change triggers reconciliation of all cells using that template
- Cell specs update to match new template's values
- Gateway deployments scale to match new template

**Teardown:** Patch back to original template name.

### update-template
**Tier:** standard
**Applicable:** Template-based fixtures (`templated-full`)

**How to patch:**
1. Read the cellTemplate name from the cluster's `templateDefaults`.
2. Patch the CellTemplate CR directly (not the MultigresCluster):
   ```bash
   kubectl patch celltemplate <name> -n <ns> --type=merge -p '{"spec":{"multigateway":{"replicas":3}}}'
   ```

**What to observe:**
- Template content change propagates to all cells using it
- Cells reconcile with updated values

**Teardown:** Patch template back to original values.

### template-affinity-propagation
**Tier:** standard | **Fast-path:** no
**Tests:** Affinity settings in templates propagate to all managed pods
**Fixtures:** templated-full

**How to execute:**
1. Deploy the `templated-full` fixture which includes affinity in its template.
2. Wait for Healthy phase.
3. **Verify pool pods**:
   ```
   kubectl get pods -n <ns> -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.affinity}{"\n"}{end}'
   ```
4. **Verify multiorch pods**:
   ```
   kubectl get pods -n <ns> -l app.kubernetes.io/component=multiorch -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.affinity}{"\n"}{end}'
   ```
5. **Verify multigateway pods**:
   ```
   kubectl get pods -n <ns> -l app.kubernetes.io/component=multigateway -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.affinity}{"\n"}{end}'
   ```
6. Compare each pod's affinity spec against the template's affinity definition.

**Success criteria:**
- All pool, multiorch, and multigateway pods have the affinity from the template
- Override precedence is respected (shard override > template)
- Run Template Verification Protocol (TVP) for full validation

**Teardown:** Standard fixture teardown.
