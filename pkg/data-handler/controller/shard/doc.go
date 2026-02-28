/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package shard implements the data-plane controller for the Shard resource.
//
// While the resource-handler Shard controller manages Kubernetes-native objects
// (Pods, Services, ConfigMaps, PVCs, and MultiOrch Deployments), this controller
// handles Multigres-specific data plane operations that require direct interaction
// with etcd topology and gRPC calls to running MultiPooler instances.
//
// # Database Topology Management
//
// The controller registers and maintains database metadata in the global etcd
// topology. Each Shard maps to a database/tablegroup/shard tuple that is
// registered in etcd so that MultiOrch and MultiGateway can discover and route
// to the correct PostgreSQL pools. On deletion, the controller unregisters the
// database from the topology, ensuring clean teardown.
//
// # Drain State Machine
//
// When pods are marked for draining (scale-down or rolling update), this
// controller executes a multi-step state machine (see drain.go) that:
//
//   - Identifies the pod's role (PRIMARY or REPLICA) from the topology.
//   - For PRIMARY pods: initiates a planned failover via gRPC before proceeding.
//   - Breaks replication by updating the standby list on the new primary.
//   - Unregisters the pod's MultiPooler record from etcd.
//   - Removes the drain annotation to allow the resource-handler to delete the pod.
//
// The state machine is idempotent and checkpoint-based, persisting progress in
// pod annotations so it can resume after controller restarts.
//
// # Backup Health Monitoring
//
// The controller periodically evaluates backup health (see backup_health.go) by
// querying the PRIMARY MultiPooler via gRPC for pgBackRest backup metadata. It
// sets a BackupHealthy condition on the Shard status and exports Prometheus
// metrics for alerting when backups become stale.
//
// # Relationship to the Resource-Handler Shard Controller
//
// The operator uses a two-controller pattern for each resource type:
//
//   - resource-handler: Manages Kubernetes objects (Pods, Services, PVCs, etc.)
//   - data-handler: Manages Multigres data plane state (etcd topology, gRPC calls)
//
// Both controllers watch the same Shard CR but are responsible for orthogonal
// concerns. The data-handler shard controller also watches Pods to react to
// drain annotations set by the resource-handler during scale-down operations.
//
// # Usage
//
// The controller is instantiated via [ShardReconciler] and registered with a
// controller-runtime Manager using [ShardReconciler.SetupWithManager]. For
// testing, the topology store factory can be overridden with
// [ShardReconciler.SetCreateTopoStore] and the gRPC client with
// [ShardReconciler.SetRPCClient].
package shard
