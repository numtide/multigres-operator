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

// Package status provides utilities for managing and calculating the Phase
// and Status conditions of Multigres Custom Resources.
//
// It defines shared helper functions like ComputePhase to ensure consistent
// lifecycle management across different resources such as TopoServer, Cell,
// Shard, TableGroup, and MultigresCluster.
package status

import (
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// ComputePhase determines the phase of a resource based on its readiness.
// This is a shared helper used by resources with simple replica counts (TopoServer, MultiGateway).
func ComputePhase(ready, total int32) multigresv1alpha1.Phase {
	if total == 0 {
		return multigresv1alpha1.PhaseInitializing
	}
	if ready == total {
		return multigresv1alpha1.PhaseHealthy
	}
	return multigresv1alpha1.PhaseProgressing
}
