# Postgres Config Scenarios

Scenarios for testing `postgresConfigRef` — ConfigMap-based postgresql.conf overrides and the hash-based rolling update mechanism.

**Key implementation details:**
- The operator computes a SHA-256 hash of the referenced ConfigMap key's data each reconciliation
- The hash is stored as pod annotation `multigres.com/postgres-config-hash`
- The hash is included in the spec-hash computation, so content changes trigger rolling updates
- The shard controller watches all ConfigMaps and re-enqueues shards that reference a changed ConfigMap
- pgctld receives the config via `--postgres-config-template=/etc/pgctld/postgres/postgresql.conf.tmpl`

---

### verify-postgres-config-ref
**Tier:** quick | **Fast-path:** yes
**Tests:** Baseline verification that postgresConfigRef is correctly wired — volume mounted, pgctld arg present, hash annotation set
**Applicable fixtures:** `postgres-config-ref`

**How to execute:**
1. Deploy the `postgres-config-ref` fixture (prerequisites first).
2. Wait for CRD phase `Healthy` and all pods Running+Ready.
3. Verify pgctld has the `--postgres-config-template` arg:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: {range .spec.containers[?(@.name=="postgres")].args[*]}{@}{" "}{end}{"\n"}{end}' | grep postgres-config-template
   ```
4. Verify the `postgres-config-template` volume is mounted from the correct ConfigMap:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: {range .spec.volumes[?(@.name=="postgres-config-template")]}{.configMap.name}{end}{"\n"}{end}'
   # Expected: custom-pg-config for all pods
   ```
5. Verify the `multigres.com/postgres-config-hash` annotation is present and non-empty:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations.multigres\.com/postgres-config-hash}{"\n"}{end}'
   ```
6. Verify all pods have the same hash value (they all reference the same ConfigMap key).

**Success criteria:**
- All pool pods have `--postgres-config-template=/etc/pgctld/postgres/postgresql.conf.tmpl` in pgctld args
- All pool pods have `postgres-config-template` volume sourced from `custom-pg-config` ConfigMap
- All pool pods have `multigres.com/postgres-config-hash` annotation with a 64-char hex string
- All pods share the same hash value
- Observer reports no errors

**Teardown:** Not needed.

---

### update-postgres-config-content
**Tier:** standard | **Fast-path:** no
**Tests:** Changing the referenced ConfigMap's content triggers a rolling update via the hash annotation mechanism. This is the core test for the hash-based rolling update feature.
**Applicable fixtures:** `postgres-config-ref`

**How to execute:**
1. Record the current postgres-config-hash annotation and pod names/creation timestamps:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.multigres\.com/postgres-config-hash}{"\t"}{.metadata.creationTimestamp}{"\n"}{end}'
   ```
   Save the hash value as `HASH_BEFORE` and pod names as `PODS_BEFORE`.
2. **Mutation**: Update the ConfigMap content (change `shared_buffers`):
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl patch configmap custom-pg-config -n default --type merge -p '{"data":{"postgresql.conf":"# Updated PostgreSQL configuration\nshared_buffers = '\''512MB'\''\nwork_mem = '\''16MB'\''\nmax_connections = 50\n"}}'
   ```
3. **Monitor** the rolling update. The shard controller watches ConfigMaps and will re-enqueue:
   ```bash
   watch -n5 'KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o custom-columns=NAME:.metadata.name,HASH:.metadata.annotations.multigres\\.com/postgres-config-hash,DRAIN:.metadata.annotations.drain\\.multigres\\.com/state,READY:.status.conditions[?\(@.type==\"Ready\"\)].status,AGE:.metadata.creationTimestamp --no-headers'
   ```
4. Watch for drain state transitions — pods should be replaced one at a time through the drain state machine.
5. After all pods are replaced, verify the new hash differs from `HASH_BEFORE`:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.multigres\.com/postgres-config-hash}{"\n"}{end}'
   ```
6. Verify all pod names differ from `PODS_BEFORE` (all pods were replaced).
7. Run full Stability Verification Protocol.

**Success criteria:**
- ConfigMap update triggers shard reconciliation (via ConfigMap watch)
- New `multigres.com/postgres-config-hash` annotation differs from the original
- All pool pods are replaced (new names or new creation timestamps)
- Rolling update goes through drain state machine (one pod at a time, no concurrent drains)
- New pods have the updated volume content (ConfigMap propagation)
- pgctld still has `--postgres-config-template` arg
- Observer reports no persistent errors after stabilization
- Replication re-establishes after rolling update

**What to observe:**
- Drain annotations should progress: `requested` -> `draining` -> `acknowledged` -> `ready-for-deletion`
- Only one pod should be in drain state at a time
- Primary pod should be drained last
- Watch operator logs for the "spec-hash mismatch" detection that triggers the rolling update

**Teardown:** Restore original ConfigMap content:
```bash
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl apply -f tools/observer/skills/exercise_cluster/fixtures/postgres-config-ref/prerequisites.yaml
```
Then wait for another rolling update to complete and verify stability.

---

### remove-postgres-config-ref
**Tier:** standard | **Fast-path:** no
**Tests:** Removing `postgresConfigRef` from the CR triggers a rolling update that removes the volume mount, pgctld arg, and hash annotation
**Applicable fixtures:** `postgres-config-ref`

**How to execute:**
1. Record baseline pod names and confirm `--postgres-config-template` arg is present.
2. **Mutation**: Remove `postgresConfigRef` from the shard spec using a JSON patch:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl patch multigrescluster pg-config-ref -n default --type json -p '[{"op":"remove","path":"/spec/databases/0/tablegroups/0/shards/0/spec/postgresConfigRef"}]'
   ```
3. Wait for rolling update to complete (all pods replaced).
4. Verify the `--postgres-config-template` arg is no longer present:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: {range .spec.containers[?(@.name=="postgres")].args[*]}{@}{" "}{end}{"\n"}{end}'
   # Should NOT contain --postgres-config-template
   ```
5. Verify the `postgres-config-template` volume is no longer present:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: volumes={range .spec.volumes[*]}{.name}{","}{end}{"\n"}{end}'
   # Should NOT contain postgres-config-template
   ```
6. Verify the `multigres.com/postgres-config-hash` annotation is absent:
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{range .items[*]}{.metadata.name}: hash={.metadata.annotations.multigres\.com/postgres-config-hash}{"\n"}{end}'
   # hash should be empty for all pods
   ```
7. Run full Stability Verification Protocol.

**Success criteria:**
- Removing `postgresConfigRef` triggers rolling update
- New pods do NOT have `--postgres-config-template` in pgctld args
- New pods do NOT have `postgres-config-template` volume
- New pods do NOT have `multigres.com/postgres-config-hash` annotation
- pgctld uses its built-in template (default auto-tuned values)
- Observer reports no persistent errors

**Teardown:** Re-add the ref to restore the fixture to its original state. Use JSON patch (merge patch fails because it interprets the missing `pools` key as removal):
```bash
KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl patch multigrescluster pg-config-ref -n default --type json -p '[{"op":"add","path":"/spec/databases/0/tablegroups/0/shards/0/spec/postgresConfigRef","value":{"name":"custom-pg-config","key":"postgresql.conf"}}]'
```
Wait for rolling update and verify stability.

---

### verify-postgres-config-settings
**Tier:** quick | **Fast-path:** yes
**Tests:** The custom PostgreSQL settings from the ConfigMap are applied in the running database. Verifies the full end-to-end path: operator mounts ConfigMap → pgctld renders template → PostgreSQL uses settings.
**Applicable fixtures:** `postgres-config-ref`

**Important:** The ConfigMap content is processed by pgctld as a Go template via `text/template`. Any `{{...}}` directives in the content (including in comments) will be parsed and executed. If the template contains invalid Go template syntax or references non-existent fields, `GeneratePostgresServerConfig` will fail silently and PostgreSQL will start with `initdb` defaults instead. Avoid `{{...}}` in comments — describe template usage in plain text instead.

**How to execute:**
1. Deploy the `postgres-config-ref` fixture and wait for stability.
2. Get the postgres password from the shard's password Secret:
   ```bash
   SECRET=$(KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get secrets -n default -o name | grep postgres-password)
   PGPASS=$(KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get $SECRET -n default -o jsonpath='{.data.password}' | base64 -d)
   ```
3. Verify the mounted config template file matches the ConfigMap content:
   ```bash
   POD=$(KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl get pods -n default -l app.kubernetes.io/component=shard-pool -o jsonpath='{.items[0].metadata.name}')
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n default $POD -c postgres -- cat /etc/pgctld/postgres/postgresql.conf.tmpl
   # Should match the ConfigMap content exactly
   ```
4. Verify the on-disk `postgresql.conf` is the rendered template (not initdb defaults):
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n default $POD -c postgres -- wc -l /var/lib/pooler/pg_data/postgresql.conf
   # Should be ~13 lines (rendered template), NOT ~844 lines (initdb defaults)
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n default $POD -c postgres -- grep -E "^(shared_buffers|work_mem|max_connections)" /var/lib/pooler/pg_data/postgresql.conf
   # Should show the ConfigMap values, not initdb defaults
   ```
5. Check PostgreSQL runtime settings (requires TCP + password):
   ```bash
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n default $POD -c postgres -- sh -c "PGPASSWORD='$PGPASS' psql -h 127.0.0.1 -p 5432 -U postgres -tAc 'SHOW shared_buffers'"
   # Expected: 256MB
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n default $POD -c postgres -- sh -c "PGPASSWORD='$PGPASS' psql -h 127.0.0.1 -p 5432 -U postgres -tAc 'SHOW work_mem'"
   # Expected: 16MB
   KUBECONFIG=$(pwd)/kubeconfig.yaml kubectl exec -n default $POD -c postgres -- sh -c "PGPASSWORD='$PGPASS' psql -h 127.0.0.1 -p 5432 -U postgres -tAc 'SHOW max_connections'"
   # Expected: 50
   ```

**Success criteria:**
- Mounted template file at `/etc/pgctld/postgres/postgresql.conf.tmpl` matches ConfigMap content
- On-disk `postgresql.conf` is the rendered template (~13 lines), not initdb defaults (~844 lines)
- `shared_buffers` = `256MB` (not the initdb default `128MB`)
- `work_mem` = `16MB` (not the initdb default `4MB`)
- `max_connections` = `50` (not the initdb default `100`)
- Settings are consistent across all pool pods

**Teardown:** Not needed.
