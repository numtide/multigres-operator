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
// ShardTemplateSpec Spec
// ============================================================================

// ShardTemplateSpec defines reusable config for Shard components (MultiOrch, Pools).
type ShardTemplateSpec struct {
	// MultiOrch configuration.
	// +optional
	MultiOrch *MultiOrchSpec `json:"multiorch,omitempty"`

	// +optional
	// +kubebuilder:validation:MaxProperties=8
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) < 63)",message="pool names must be < 63 chars"
	Pools map[string]PoolSpec `json:"pools,omitempty"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// ShardTemplate is the Schema for the shardtemplates API
// +kubebuilder:resource:shortName=sht
type ShardTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ShardTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ShardTemplateList contains a list of ShardTemplate
type ShardTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShardTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ShardTemplate{}, &ShardTemplateList{})
}
