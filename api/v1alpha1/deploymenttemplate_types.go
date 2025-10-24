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
// DeploymentTemplateSpec Spec (User-editable API)
// ============================================================================

// DeploymentTemplateSpec defines the desired state of DeploymentTemplate
// These are user editable and watched by MultigresCluster controller ONLY when referenced.
// +kubebuilder:validation:XValidation:Rule="has(self.shardPool) || has(self.multiOrch) || has(self.multiGateway) || has(self.multiAdmin) || has(self.images) || has(self.managedTopoServer)",Message="a deployment template must define at least one template spec (e.g., shardPool, images, etc.)"
type DeploymentTemplateSpec struct {
	// ShardPool is the template for a MultiShard pool.
	// +optional
	ShardPool *ShardPoolSpec `json:"shardPool,omitempty"`

	// MultiOrch is the template for a MultiOrch deployment.
	// +optional
	MultiOrch *StatelessSpec `json:"multiorch,omitempty"`

	// MultiGateway is the template for a MultiGateway deployment.
	// +optional
	MultiGateway *StatelessSpec `json:"multigateway,omitempty"`

	// MultiAdmin is the template for a MultiAdmin deployment.
	// +optional
	MultiAdmin *StatelessSpec `json:"multiadmin,omitempty"`

	// Images is the template for all container images.
	// +optional
	Images *ImagesTemplateSpec `json:"images,omitempty"`

	// ManagedTopoServer is the template for a managed TopoServer.
	// +optional
	ManagedTopoServer *TopoServerSpec `json:"managedTopoServer,omitempty"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================
// A MultigresCluster controller updates this status when a reference to this template is created or removed.

// DeploymentTemplateStatus defines the observed state of DeploymentTemplate
type DeploymentTemplateStatus struct {
	// Consumers is a list of MultigresCluster resources that are
	// currently referencing this template.
	// This can be used by a validating webhook to prevent deletion while in use.
	// +optional
	Consumers []ConsumerRef `json:"consumers,omitempty"`
}

// ConsumerRef holds a reference to a MultigDrivesCluster CR that is using this template.
type ConsumerRef struct {
	// Name of the consuming cluster.
	Name string `json:"name"`
	// Namespace of the consuming cluster.
	Namespace string `json:"namespace"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Consumers",type="integer",JSONPath=".status.consumers.#",description="Number of clusters using this template"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DeploymentTemplate is the Schema for the DeploymentTemplates API
type DeploymentTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeploymentTemplateSpec   `json:"spec,omitempty"`
	Status DeploymentTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DeploymentTemplateList contains a list of DeploymentTemplate
type DeploymentTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeploymentTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DeploymentTemplate{}, &DeploymentTemplateList{})
}
