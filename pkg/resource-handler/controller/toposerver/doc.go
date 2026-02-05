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

// Package toposerver implements the controller for the TopoServer resource.
//
// The TopoServer controller manages the etcd-based topology storage that Vitess
// components use for cluster coordination and service discovery. Its responsibilities
// include:
//
// # StatefulSet Management
//
// Creates and maintains a StatefulSet running etcd with:
//   - Persistent storage for topology data
//   - Proper clustering configuration for multi-replica deployments
//   - Container environment variables for etcd peer discovery
//
// # Service Management
//
// The controller creates two services:
//   - Headless Service: Enables peer discovery between etcd replicas using stable
//     DNS names (required for etcd clustering).
//   - Client Service: Provides a stable endpoint for Vitess components to connect
//     to the topology storage.
//
// # Status Aggregation
//
// Monitors the StatefulSet's ready replicas and updates the TopoServer status
// with current replica counts and health conditions.
//
// TopoServer resources are typically created by the MultigresCluster controller
// when the cluster is configured with managed topology (as opposed to an external
// etcd cluster).
package toposerver
