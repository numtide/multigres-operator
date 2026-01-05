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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================================
// RBAC Markers (Temporary Location)
// ============================================================================
//
// TODO: Move these RBAC markers to the controller implementation
// (pkg/resource-handler/controller/shard/shard_controller.go)
// to follow kubebuilder conventions. They are temporarily placed here because
// controller-gen cannot process files in go.work modules.
//
// +kubebuilder:rbac:groups=multigres.com,resources=shards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=shards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=shards/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// ============================================================================
// Shard Component Specs (Reusable)
// ============================================================================
//
// These specs define the specific components of a shard (Orchestration and Data Pools).
// They are used by ShardTemplate, TableGroup, and the Shard Child CR.

// MultiOrchSpec defines the configuration specifically for MultiOrch,
// which requires placement logic (cell targeting).
type MultiOrchSpec struct {
	StatelessSpec `json:",inline"`

	// Cells defines the list of cells where this MultiOrch should be deployed.
	// If empty, it defaults to all cells where pools are defined.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	Cells []CellName `json:"cells,omitempty"`
}

// PoolSpec defines the configuration for a data pool (StatefulSet).
type PoolSpec struct {
	// Type of the pool (e.g., "readWrite", "readOnly").
	// +kubebuilder:validation:Enum=readWrite;readOnly
	// +optional
	Type string `json:"type,omitempty"`

	// Cells defines the list of cells where this Pool should be deployed.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	Cells []CellName `json:"cells,omitempty"`

	// ReplicasPerCell is the desired number of pods PER CELL in this pool.
	// +kubebuilder:validation:Minimum=0
	// +optional
	ReplicasPerCell *int32 `json:"replicasPerCell,omitempty"`

	// Storage defines the storage configuration for the pool's data volumes.
	// +optional
	Storage StorageSpec `json:"storage,omitempty"`

	// Postgres container configuration.
	// +optional
	Postgres ContainerConfig `json:"postgres,omitempty"`

	// Multipooler container configuration.
	// +optional
	Multipooler ContainerConfig `json:"multipooler,omitempty"`

	// Affinity defines the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// ============================================================================
// Shard Spec (Read-only API)
// ============================================================================
//
// Shard is a child CR managed by TableGroup.
// Represents a single logical shard with its orchestration and pools.

// ShardSpec defines the desired state of Shard.
type ShardSpec struct {
	// DatabaseName is the name of the logical database this shard belongs to.
	// +kubebuilder:validation:MaxLength=63
	DatabaseName string `json:"databaseName"`

	// TableGroupName is the name of the table group this shard belongs to.
	// +kubebuilder:validation:MaxLength=63
	TableGroupName string `json:"tableGroupName"`

	// ShardName is the specific identifier for this shard (e.g. "0").
	// +kubebuilder:validation:MaxLength=63
	ShardName string `json:"shardName"`

	// Images defines the container images to be used by this shard (defined globally at MultigresCluster).
	Images ShardImages `json:"images"`

	// GlobalTopoServer is a reference to the global topology server.
	GlobalTopoServer GlobalTopoServerRef `json:"globalTopoServer"`

	// MultiOrch is the fully resolved configuration for the shard orchestrator.
	MultiOrch MultiOrchSpec `json:"multiorch"`

	// Pools is the map of fully resolved data pool configurations.
	// +kubebuilder:validation:MaxProperties=8
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) < 63)",message="pool names must be < 63 chars"
	Pools map[string]PoolSpec `json:"pools"`
}

// ShardImages defines the images required for a Shard.
type ShardImages struct {
	// ImagePullPolicy overrides the default image pull policy.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets is a list of references to secrets in the same namespace.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// MultiOrch is the image for the shard orchestrator.
	// +kubebuilder:validation:MaxLength=512
	MultiOrch string `json:"multiorch"`

	// MultiPooler is the image for the connection pooler sidecar.
	// +kubebuilder:validation:MaxLength=512
	MultiPooler string `json:"multipooler"`

	// Postgres is the image for the postgres database.
	// +kubebuilder:validation:MaxLength=512
	Postgres string `json:"postgres"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// ShardStatus defines the observed state of Shard.
type ShardStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Cells is a list of cells this shard is currently deployed to.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	Cells []CellName `json:"cells,omitempty"`

	// OrchReady indicates if the MultiOrch component is ready.
	OrchReady bool `json:"orchReady"`

	// PoolsReady indicates if all data pools are ready.
	PoolsReady bool `json:"poolsReady"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status"

// Shard is the Schema for the shards API
type Shard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShardSpec   `json:"spec,omitempty"`
	Status ShardStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ShardList contains a list of Shard
type ShardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Shard `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Shard{}, &ShardList{})
}
