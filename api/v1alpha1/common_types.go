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
)

// ============================================================================
// Shared Configuration Structs
// ============================================================================
//
// These structs are used across multiple resources (Cluster, Templates, Children)
// to ensure consistency in configuration shapes.

// StatelessSpec defines the desired state for a scalable, stateless component
// like MultiAdmin, MultiOrch, or MultiGateway.
type StatelessSpec struct {
	// Replicas is the desired number of pods.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources defines the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Affinity defines the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// PodAnnotations are annotations to add to the pods.
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) < 64 && size(self[k]) < 256)",message="annotation keys must be <64 chars and values <256 chars"
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// PodLabels are additional labels to add to the pods.
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) < 64 && size(self[k]) < 64)",message="label keys and values must be <64 chars"
	PodLabels map[string]string `json:"podLabels,omitempty"`
}

// StorageSpec defines the storage configuration.
type StorageSpec struct {
	// Size of the persistent volume.
	// +kubebuilder:validation:Pattern="^([0-9]+)(.+)$"
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Size string `json:"size,omitempty"`

	// Class is the StorageClass name.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Class string `json:"class,omitempty"`
}

// ContainerConfig defines generic container configuration.
type ContainerConfig struct {
	// Resources defines the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}
