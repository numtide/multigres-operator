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
// Shard Spec (Read-only API)
// ============================================================================

// ShardSpec defines the desired state of Shard
// This spec is populated by the MultiTableGroup controller.
// +kubebuilder:validation:XValidation:rule="has(self.pools) && size(self.pools) > 0",message="at least one shard pool must be defined"
type ShardSpec struct {
	// Images required for this shard's pods.
	// +optional
	Images ShardImagesSpec `json:"images,omitempty"`

	// Pools defines the different pools of pods for this shard (e.g., replicas, read-only).
	// This is a direct copy from the parent MultiTableGroup's shardTemplate.
	// +optional
	Pools []ShardPoolSpec `json:"pools,omitempty"`
}

// ShardImagesSpec defines the images required for a Shard.
type ShardImagesSpec struct {
	// +optional
	MultiPooler string `json:"multipooler,omitempty"`
	// +optional
	Postgres string `json:"postgres,omitempty"`
}

// ShardPoolSpec defines the desired state of a pool of shard replicas (e.g., primary, replica, read-only).
// This is the core reusable spec for a shard's pod.
// +kubebuilder:validation:XValidation:rule="!has(self.dataVolumeClaimTemplate) || (has(self.dataVolumeClaimTemplate.resources) && has(self.dataVolumeClaimTemplate.resources.requests) && has(self.dataVolumeClaimTemplate.resources.requests['storage']))",message="dataVolumeClaimTemplate must include a 'storage' resource request"
type ShardPoolSpec struct {
	// Type of the pool (e.g., "replica", "readOnly").
	// +kubebuilder:validation:Enum=replica;readOnly
	// +optional
	Type string `json:"type,omitempty"`

	// Cell is the name of the Cell this pool should run in.
	// +optional
	// +kubebuilder:validation:MaxLength:=63
	// +kubebuilder:validation:Pattern:="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Cell string `json:"cell,omitempty"`

	// Replicas is the desired number of pods in this pool.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Affinity defines the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// DataVolumeClaimTemplate provides a spec for the PersistentVolumeClaim
	// that will be created for each replica.
	// +optional
	DataVolumeClaimTemplate corev1.PersistentVolumeClaimSpec `json:"dataVolumeClaimTemplate,omitempty"`

	// Postgres defines the configuration for the Postgres container.
	// +optional
	Postgres PostgresSpec `json:"postgres,omitempty"`

	// MultiPooler defines the configuration for the MultiPooler container.
	// +optional
	MultiPooler MultiPoolerSpec `json:"multipooler,omitempty"`
}

// PostgresSpec defines the configuration for the Postgres container.
type PostgresSpec struct {
	// Resources defines the compute resource requirements for the Postgres container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// MultiPoolerSpec defines the configuration for the MultiPooler container.
type MultiPoolerSpec struct {
	// Resources defines the compute resource requirements for the MultiPooler container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// ShardStatus defines the observed state of Shard
type ShardStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Shard's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// PrimaryCell is the cell currently holding the primary replica for this shard.
	// +optional
	PrimaryCell string `json:"primaryCell,omitempty"`

	// TotalPods is the total number of pods managed by this shard across all pools.
	// +optional
	TotalPods int32 `json:"totalPods,omitempty"`

	// ReadyPods is the number of pods for this shard that are ready.
	// +optional
	ReadyPods int32 `json:"readyPods,omitempty"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status",description="Current availability status"
// +kubebuilder:printcolumn:name="Primary Cell",type="string",JSONPath=".status.primaryCell",description="Cell of the primary replica"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyPods",description="Ready pods"
// +kubebuilder:printcolumn:name="Total",type="string",JSONPath=".status.totalPods",description="Total pods"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Shard is the Schema for the Shards API
type Shard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShardSpec   `json:"spec,omitempty"`
	Status ShardStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ShardList contains a list of Shard
type ShardList struct {
	metav1.TypeMeta `        json:",inline"`
	metav1.ListMeta `        json:"metadata,omitempty"`
	Items           []Shard `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Shard{}, &ShardList{})
}
