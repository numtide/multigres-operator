# Scenario Index

Quick reference for all mutation scenarios. Read the scenario's file for full details before executing.

> **General rules for all scenarios:**
> 1. Read the live CR first — `kubectl get multigrescluster <name> -n <ns> -o yaml`
> 2. Construct patches from the actual CR structure, not hardcoded paths
> 3. All kubectl commands use `KUBECONFIG=$(pwd)/kubeconfig.yaml`
> 4. Reusable patch scripts are in `patches/` — check there before constructing inline

## Core Mode Scenarios

File: `scenarios/core.md`

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| scale-up-pool-replicas | standard | yes | all |
| scale-down-pool-replicas | standard | no | all (replicasPerCell > 3) |
| scale-multigateway-replicas | standard | yes | all |
| update-resource-limits | standard | yes | all |
| delete-pool-pod | standard | yes | all |

## Config & Fixture-Specific Scenarios

File: `scenarios/config.md`

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| add-pod-annotations | quick | yes | all |
| change-images | standard | no | all |
| add-tolerations | standard | no | minimal-retain, minimal-delete |
| add-affinity | standard | no | minimal-retain, minimal-delete |
| delete-operator-pod | quick | yes | all |
| change-pvc-deletion-policy | quick | no | minimal-retain, minimal-delete |
| expand-pvc-storage | standard | yes | minimal-retain, minimal-delete |
| add-cell | lifecycle | no | all (append-only) |
| add-pool | lifecycle | no | all (append-only) |
| scale-etcd | standard | no | fixtures with inline etcd config |

## Lifecycle & Drain Scenarios

File: `scenarios/lifecycle.md`

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| delete-and-recreate-cluster | lifecycle | no | all |
| multiadmin-scale | standard | yes | overrides-complex, templated-full |
| drain-abort-on-scale | standard | no | all (replicasPerCell >= 2) |
| drain-primary-scale-down | lifecycle | no | minimal-retain, minimal-delete (replicasPerCell >= 4) |
| rolling-update-drain-path | standard | no | all |

## Template & Override Scenarios

File: `scenarios/template.md`

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| update-core-template-cr | standard | no | templated-full, overrides-complex |
| update-cell-template-cr | standard | no | templated-full, overrides-complex |
| update-shard-template-pool | standard | no | templated-full |
| override-wins-over-template | standard | no | overrides-complex |
| template-pod-labels-merge | quick | no | fixtures with template+override podLabels |
| switch-template | lifecycle | no | templated-full |
| update-template | standard | no | templated-full |
| template-affinity-propagation | standard | no | templated-full |

## Postgres Config Scenarios

File: `scenarios/postgres-config.md`

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| verify-postgres-config-ref | quick | yes | postgres-config-ref |
| update-postgres-config-content | standard | no | postgres-config-ref |
| remove-postgres-config-ref | standard | no | postgres-config-ref |
| verify-postgres-config-settings | quick | yes | postgres-config-ref |

## Concurrent Mutation Scenarios

File: `scenarios/concurrent.md` — Full mode only

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| concurrent-scale-and-delete-pod | standard | no | minimal-retain, minimal-delete, templated-full |
| concurrent-config-and-scale | standard | no | minimal-retain, minimal-delete, templated-full |
| concurrent-template-and-pod-delete | standard | no | templated-full |
| concurrent-dual-scale | standard | no | minimal-retain, minimal-delete, templated-full |

## Webhook Rejection Scenarios

File: `scenarios/webhook.md` — Negative tests (mutation MUST be rejected)

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| storage-shrink-rejection | standard | yes | all |
| etcd-replicas-immutable | standard | yes | all |
| invalid-template-reference | standard | yes | all |
| invalid-pool-name | standard | yes | all |
| resource-limits-violated | standard | yes | all |

## Failure Injection Scenarios

File: `scenarios/failure-injection.md`

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| etcd-unavailability | standard | no | all |
| image-pull-backoff | standard | no | all |

## Specialized Scenarios

File: `scenarios/specialized.md`

| Scenario | Tier | Fast-path | Fixtures |
|---|---|---|---|
| verify-topology-placement | quick | yes | multi-cell-topology |
| verify-durability-policy | quick | yes | multi-cell-quorum |
| change-log-levels | standard | no | log-levels |
| large-scale-up-pool | lifecycle | no | minimal-delete, minimal-retain, multi-pool |
| large-scale-multigateway | standard | yes | minimal-delete, minimal-retain |
| large-scale-multiadmin | standard | yes | multiadmin-lifecycle, overrides-complex |
| change-annotations-stateless | standard | no | minimal-delete, minimal-retain |
| verify-multi-pool | quick | yes | multi-pool |
| verify-external-adminweb | quick | yes | external-adminweb |
| external-adminweb-enable-disable | quick | no | external-adminweb |
| external-adminweb-change-annotations | quick | yes | external-adminweb |
