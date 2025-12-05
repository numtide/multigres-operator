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
// TableGroup Spec (Read-only API)
// ============================================================================
//
// TableGroup is a child CR managed by MultigresCluster.
// It acts as the middle-manager for Shards, holding fully resolved specs.

// TableGroupSpec defines the desired state of TableGroup.
type TableGroupSpec struct {
	DatabaseName   string `json:"databaseName"`
	TableGroupName string `json:"tableGroupName"`

	// Images required for child shards.
	Images ShardImages `json:"images"`

	// GlobalTopoServer reference.
	GlobalTopoServer GlobalTopoServerRef `json:"globalTopoServer"`

	// Shards is the list of FULLY RESOLVED shard specifications.
	Shards []ShardResolvedSpec `json:"shards"`
}

// ShardResolvedSpec represents the fully calculated spec for a shard,
// pushed down to the TableGroup.
type ShardResolvedSpec struct {
	Name string `json:"name"`

	// MultiOrch fully resolved spec.
	MultiOrch MultiOrchSpec `json:"multiorch"`

	// Pools fully resolved spec.
	Pools map[string]PoolSpec `json:"pools"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// TableGroupStatus defines the observed state of TableGroup.
type TableGroupStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

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
