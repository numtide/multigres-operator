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

// NOTE: json tags are required.  Any new fields you add must have json tags for
// the fields to be serialized.

// MultiGatewaySpec defines the desired state of MultiGateway.
type MultiGatewaySpec struct {
	// Image is the container image for MultiGateway.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:default="multigres/multigateway:latest"
	// +optional
	Image string `json:"image,omitempty"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace
	// to use for pulling the image.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Replicas is the desired number of MultiGateway pods.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources defines the resource requirements for the MultiGateway container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use for the MultiGateway pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ServiceType determines how the MultiGateway Service is exposed.
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +kubebuilder:default="ClusterIP"
	// +optional
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// HTTPPort is the port for HTTP traffic.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=15100
	// +optional
	HTTPPort int32 `json:"httpPort,omitempty"`

	// GRPCPort is the port for gRPC traffic.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=15170
	// +optional
	GRPCPort int32 `json:"grpcPort,omitempty"`

	// PostgresPort is the port for PostgreSQL protocol traffic.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=15432
	// +optional
	PostgresPort int32 `json:"postgresPort,omitempty"`

	// ServiceAnnotations are annotations to add to the MultiGateway Service.
	// +optional
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

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

	// PodAnnotations are annotations to add to the MultiGateway pods.
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// PodLabels are additional labels to add to the MultiGateway pods.
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
}

// MultiGatewayStatus defines the observed state of MultiGateway.
type MultiGatewayStatus struct {
	// Ready indicates whether the MultiGateway is healthy and available.
	Ready bool `json:"ready"`

	// Replicas is the desired number of replicas.
	Replicas int32 `json:"replicas"`

	// ReadyReplicas is the number of ready replicas.
	ReadyReplicas int32 `json:"readyReplicas"`

	// ObservedGeneration reflects the generation of the most recently observed MultiGateway spec.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the MultiGateway's state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Replicas",type=string,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MultiGateway is the Schema for the multigateways API
type MultiGateway struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of MultiGateway
	// +required
	Spec MultiGatewaySpec `json:"spec"`

	// status defines the observed state of MultiGateway
	// +optional
	Status MultiGatewayStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// MultiGatewayList contains a list of MultiGateway
type MultiGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MultiGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MultiGateway{}, &MultiGatewayList{})
}
