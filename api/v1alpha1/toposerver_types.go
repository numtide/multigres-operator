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
// (pkg/resource-handler/controller/toposerver/toposerver_controller.go)
// to follow kubebuilder conventions. They are temporarily placed here because
// controller-gen cannot process files in go.work modules.
//
// +kubebuilder:rbac:groups=multigres.com,resources=toposervers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=toposervers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=toposervers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// ============================================================================
// TopoServer Spec (Read-only API)
// ============================================================================
//
// TopoServer is a child CR managed by MultigresCluster (Global) or Cell (Local).

// EndpointUrl is a string restricted to 2048 characters for strict validation budgeting.
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=2048
type EndpointUrl string

// TopoServerSpec defines the desired state of TopoServer (Child CR).
// +kubebuilder:validation:XValidation:rule="has(self.etcd)",message="must specify 'etcd' configuration"
type TopoServerSpec struct {
	// Etcd defines the configuration if using Etcd.
	Etcd *EtcdSpec `json:"etcd,omitempty"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// TopoServerStatus defines the observed state of TopoServer.
type TopoServerStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ClientService is the name of the service for clients.
	// +optional
	ClientService string `json:"clientService,omitempty"`

	// PeerService is the name of the service for peers.
	// +optional
	PeerService string `json:"peerService,omitempty"`
}

// ============================================================================
// TopoServer Component Specs
// ============================================================================
//
// These components are not directly used in the formation of this child CR,
// but they are used to configure the toposerver via the MultigresCluster

// EtcdSpec defines the configuration for a managed Etcd cluster.
type EtcdSpec struct {
	// Image is the Etcd container image.
	// +optional
	Image ImageRef `json:"image,omitempty"`

	// Replicas is the desired number of etcd members.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Storage configuration for Etcd data.
	// +optional
	Storage StorageSpec `json:"storage,omitempty"`

	// Resources defines the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// RootPath is the etcd prefix for this cluster.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	RootPath string `json:"rootPath,omitempty"`
}

// GlobalTopoServerSpec defines the configuration for the global topology server.
// It can be either an inline Etcd spec, an External reference, or a Template reference.
// +kubebuilder:validation:XValidation:rule="[has(self.etcd), has(self.external), has(self.templateRef)].filter(x, x).size() == 1",message="must specify exactly one of 'etcd', 'external', or 'templateRef'"
type GlobalTopoServerSpec struct {
	// Etcd defines an inline managed Etcd cluster.
	// +optional
	Etcd *EtcdSpec `json:"etcd,omitempty"`

	// External defines connection details for an unmanaged, external topo server.
	// +optional
	External *ExternalTopoServerSpec `json:"external,omitempty"`

	// TemplateRef refers to a CoreTemplate to load configuration from.
	// +optional
	TemplateRef TemplateRef `json:"templateRef,omitempty"`
}

// ExternalTopoServerSpec defines connection details for an external system.
type ExternalTopoServerSpec struct {
	// Endpoints is a list of client URLs.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	// +kubebuilder:validation:XValidation:rule="self.all(x, x.matches('^https?://'))",message="endpoints must be valid URLs"
	Endpoints []EndpointUrl `json:"endpoints"`

	// CASecret is the name of the secret containing the CA certificate.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	CASecret string `json:"caSecret,omitempty"`

	// ClientCertSecret is the name of the secret containing the client cert/key.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	ClientCertSecret string `json:"clientCertSecret,omitempty"`

	// RootPath is the etcd prefix for this cluster.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	RootPath string `json:"rootPath,omitempty"`
}

// LocalTopoServerSpec defines configuration for Cell-local topology.
// +kubebuilder:validation:XValidation:rule="has(self.etcd) || has(self.external)",message="must specify either 'etcd' or 'external'"
// +kubebuilder:validation:XValidation:rule="!(has(self.etcd) && has(self.external))",message="only one of 'etcd' or 'external' can be set"
type LocalTopoServerSpec struct {
	// Etcd defines an inline managed Etcd cluster.
	// +optional
	Etcd *EtcdSpec `json:"etcd,omitempty"`

	// External defines connection details for an unmanaged, external topo server.
	// +optional
	External *ExternalTopoServerSpec `json:"external,omitempty"`
}

// GlobalTopoServerRef defines a reference to the global topo server.
// Used by Cell, TableGroup, and Shard.
type GlobalTopoServerRef struct {
	// Address is the DNS address of the topology server.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Address string `json:"address"`

	// RootPath is the etcd prefix for this cluster.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	RootPath string `json:"rootPath"`

	// Implementation defines the client implementation (e.g. "etcd").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Implementation string `json:"implementation"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status"

// TopoServer is the Schema for the toposervers API
// +kubebuilder:resource:shortName=tps
type TopoServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TopoServerSpec   `json:"spec,omitempty"`
	Status TopoServerStatus `json:"status,omitempty"`
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
