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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================================
// RBAC Markers (Temporary Location)
// ============================================================================
//
// TODO: Move these RBAC markers to the controller implementation
// (pkg/cluster-handler/controller/tablegroup/tablegroup_controller.go)
// to follow kubebuilder conventions. They are temporarily placed here because
// controller-gen cannot process files in go.work modules.
//
// +kubebuilder:rbac:groups=multigres.com,resources=tablegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=tablegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=shards,verbs=get;list;watch;create;update;patch;delete

// ============================================================================
// TableGroup Spec (Read-only API)
// ============================================================================
//
// TableGroup is a child CR managed by MultigresCluster.
// It acts as the middle-manager for Shards, holding fully resolved specs.

// TableGroupSpec defines the desired state of TableGroup.
type TableGroupSpec struct {
	// DatabaseName is the name of the logical database.
	DatabaseName DatabaseName `json:"databaseName"`

	// TableGroupName is the name of this table group.
	TableGroupName TableGroupName `json:"tableGroupName"`

	// IsDefault indicates if this is the default/unsharded group for the database.
	// +optional
	IsDefault bool `json:"default,omitempty"`

	// Images defines the container images used for child shards - defined globally in MultigresCluster.
	Images ShardImages `json:"images"`

	// GlobalTopoServer is a reference to the global topology server.
	GlobalTopoServer GlobalTopoServerRef `json:"globalTopoServer"`

	// Shards is the list of FULLY RESOLVED shard specifications.
	// +kubebuilder:validation:MaxItems=32
	Shards []ShardResolvedSpec `json:"shards"`
}

// ShardResolvedSpec represents the fully calculated spec for a shard,
// pushed down to the TableGroup.
type ShardResolvedSpec struct {
	// Name is the identifier of the shard.
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// MultiOrch is the fully resolved configuration for the orchestrator.
	MultiOrch MultiOrchSpec `json:"multiorch"`

	// Pools is the map of fully resolved data pool configurations.
	// +kubebuilder:validation:MaxProperties=8
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) < 63)",message="pool names must be < 63 chars"
	Pools map[PoolName]PoolSpec `json:"pools"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// TableGroupStatus defines the observed state of TableGroup.
type TableGroupStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the aggregated lifecycle state of the table group.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// Message provides details about the current phase.
	// +optional
	Message string `json:"message,omitempty"`

	ReadyShards int32 `json:"readyShards"`
	TotalShards int32 `json:"totalShards"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Shards",type="integer",JSONPath=".status.readyShards"

// TableGroup is the Schema for the tablegroups API
// +kubebuilder:resource:shortName=tbg
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
type TableGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TableGroupSpec   `json:"spec,omitempty"`
	Status TableGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TableGroupList contains a list of TableGroup
type TableGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TableGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TableGroup{}, &TableGroupList{})
}
