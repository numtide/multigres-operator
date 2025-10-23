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

// TopoServerSpec defines the desired state of a managed etcd cluster.
// This is reusable for both Global and Local TopoServers.
type TopoServerSpec struct {
	// Image is the etcd container image to use.
	// +optional
	Image string `json:"image,omitempty"`

	// Replicas is the desired number of etcd pods.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Affinity defines the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// DataVolumeClaimTemplate provides a spec for the PersistentVolumeClaim
	// that will be created for each etcd replica.
	// +optional
	DataVolumeClaimTemplate corev1.PersistentVolumeClaimSpec `json:"dataVolumeClaimTemplate,omitempty"`
}

// TopoServerChildSpec defines the desired state of TopoServer
// This spec is populated by the MultigresCluster controller.
type TopoServerChildSpec struct {
	// RootPath is the root path to use within the etcd cluster.
	RootPath string `json:"rootPath"`

	// TopoServerSpec contains the reusable spec for deploying an etcd cluster.
	TopoServerSpec `json:",inline"`
}

// TopoServerStatus defines the observed state of TopoServer
type TopoServerStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the TopoServer's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Replicas is the current number of etcd pods.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of etcd pods ready to serve requests.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// ClientServiceName is the name of the service for etcd clients.
	// +optional
	ClientServiceName string `json:"clientServiceName,omitempty"`

	// PeerServiceName is the name of the service for etcd peer communication.
	// +optional
	PeerServiceName string `json:"peerServiceName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status",description="Current availability status"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyReplicas",description="Ready replicas"
// +kubebuilder:printcolumn:name="Total",type="string",JSONPath=".status.replicas",description="Total replicas"
// +kubebuilder:printcolumn:name="Service",type="string",JSONPath=".status.clientServiceName",description="Client Service"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TopoServer is the Schema for the toposervers API
type TopoServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TopoServerChildSpec `json:"spec,omitempty"`
	Status TopoServerStatus    `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TopoServerList contains a list of TopoServer
type TopoServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TopoServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TopoServer{}, &TopoServerList{})
}
