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

// Package cell implements the data-plane controller for the Cell resource.
//
// While the resource-handler Cell controller manages Kubernetes-native objects
// (Deployments and Services for MultiGateway), this controller handles the
// Multigres-specific data plane operations that require direct interaction with
// the etcd-based topology store.
//
// # Responsibilities
//
//   - Cell Registration: Registers cell metadata in the global etcd topology
//     using the upstream multigres topoclient library. This makes cells visible
//     to other Multigres components (MultiOrch, MultiGateway) for routing and
//     orchestration decisions.
//
//   - Topology Cleanup: Removes cell metadata from the global topology when a
//     Cell resource is deleted, ensuring no stale entries remain in etcd.
//
//   - Startup Resilience: Handles topology server unavailability during cluster
//     bootstrap by silently requeuing reconciliation within a configurable grace
//     period, avoiding false error reports while etcd is still starting.
//
// # Relationship to the Resource-Handler Cell Controller
//
// The operator uses a two-controller pattern for each resource type:
//
//   - resource-handler: Manages Kubernetes objects (Deployments, Services, etc.)
//   - data-handler: Manages Multigres data plane state (etcd topology, gRPC calls)
//
// Both controllers watch the same Cell CR but are responsible for orthogonal
// concerns. The data-handler cell controller runs in the data-handler binary
// alongside other data-plane controllers.
//
// # Usage
//
// The controller is instantiated via [CellReconciler] and registered with a
// controller-runtime Manager using [CellReconciler.SetupWithManager]. For
// testing, the topology store factory can be overridden with
// [CellReconciler.SetCreateTopoStore].
package cell
