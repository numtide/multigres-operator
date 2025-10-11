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

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MultiPoolerSpec defines the desired state of MultiPooler.
type MultiPoolerSpec struct {
	// CellName is the name of the cell this MultiPooler belongs to.
	// +kubebuilder:validation:MinLength=1
	// +optional
	CellName string `json:"cellName,omitempty"`

	// Replicas is the desired number of MultiPooler pods.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// MultiPooler defines the configuration for the multipooler container.
	// +optional
	MultiPooler MultiPoolerContainerSpec `json:"multiPooler,omitempty"`

	// Pgctld defines the configuration for the pgctld container.
	// +optional
	Pgctld MultiPoolerContainerSpec `json:"pgctld,omitempty"`

	// Postgres defines the configuration for the postgres container.
	// +optional
	Postgres MultiPoolerContainerSpec `json:"postgres,omitempty"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace
	// to use for pulling images.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use for the MultiPooler pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// HTTPPort is the port for HTTP traffic.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=15200
	// +optional
	HTTPPort int32 `json:"httpPort,omitempty"`

	// GRPCPort is the port for gRPC traffic.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=15270
	// +optional
	GRPCPort int32 `json:"grpcPort,omitempty"`

	// PostgresPort is the port for PostgreSQL protocol traffic.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=5432
	// +optional
	PostgresPort int32 `json:"postgresPort,omitempty"`

	// StorageClassName is the name of the StorageClass to use for PostgreSQL data volumes.
	// If not specified, the default StorageClass will be used.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// StorageSize is the size of the persistent volume for each MultiPooler pod.
	// +kubebuilder:default="10Gi"
	// +optional
	StorageSize string `json:"storageSize,omitempty"`

	// VolumeClaimTemplate allows customization of the PersistentVolumeClaim for PostgreSQL data.
	// If specified, this takes precedence over StorageClassName and StorageSize.
	// +optional
	VolumeClaimTemplate *corev1.PersistentVolumeClaimSpec `json:"volumeClaimTemplate,omitempty"`

	// Affinity defines pod affinity and anti-affinity rules.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations allows pods to schedule onto nodes with matching taints.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// TopologySpreadConstraints controls how pods are spread across topology domains.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// PodAnnotations are annotations to add to the MultiPooler pods.
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// PodLabels are additional labels to add to the MultiPooler pods.
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
}

// MultiPoolerStatus defines the observed state of MultiPooler.
type MultiPoolerStatus struct {
	// Ready indicates whether the MultiPooler is healthy and available.
	Ready bool `json:"ready"`

	// Replicas is the desired number of MultiPooler pods.
	Replicas int32 `json:"replicas"`

	// ReadyReplicas is the number of ready MultiPooler pods.
	ReadyReplicas int32 `json:"readyReplicas"`

	// ObservedGeneration reflects the generation of the most recently observed MultiPooler spec.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the MultiPooler's state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cell",type=string,JSONPath=`.spec.cellName`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Replicas",type=string,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MultiPooler is the Schema for the multipoolers API
type MultiPooler struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of MultiPooler
	// +required
	Spec MultiPoolerSpec `json:"spec"`

	// status defines the observed state of MultiPooler
	// +optional
	Status MultiPoolerStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// MultiPoolerList contains a list of MultiPooler
type MultiPoolerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MultiPooler `json:"items"`
}

// MultiPoolerContainerSpec defines the configuration for a container in a MultiPooler pod.
type MultiPoolerContainerSpec struct {
	// Image is the container image.
	// +kubebuilder:validation:MinLength=1
	// +optional
	Image string `json:"image,omitempty"`

	// Resources defines the resource requirements for this container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

func init() {
	SchemeBuilder.Register(&MultiPooler{}, &MultiPoolerList{})
}
