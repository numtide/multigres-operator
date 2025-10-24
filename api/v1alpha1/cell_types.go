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
// Cell Spec (Read-only API)
// ============================================================================

// CellSpec defines the desired state of Cell
// This spec is populated by the MultigresCluster controller.
type CellSpec struct {
	// Name is the logical name of the cell.
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=63
	// +kubebuilder:validation:Pattern:="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Name string `json:"name"`

	// Images required for this cell's components.
	// +optional
	Images CellImagesSpec `json:"images,omitempty"`

	// MultiGateway defines the desired state of the MultiGateway deployment.
	MultiGateway StatelessSpec `json:"multigateway"`

	// MultiOrch defines the desired state of the MultiOrch deployment.
	MultiOrch StatelessSpec `json:"multiorch"`

	// GlobalTopoServer is a reference to the cluster-wide global topo server.
	// This is always populated by the parent controller.
	GlobalTopoServer GlobalTopoServerRefSpec `json:"globalTopoServer"`

	// TopoServer defines the topology server configuration for this cell.
	TopoServer CellTopoServerSpec `json:"topoServer"`

	// AllCells is a list of all cell names in the cluster for discovery.
	// +optional
	AllCells []string `json:"allCells,omitempty"`

	// TopologyReconciliation defines flags for the cell controller's reconciliation logic.
	// +optional
	TopologyReconciliation TopologyReconciliationSpec `json:"topologyReconciliation,omitempty"`
}

// CellImagesSpec defines the images required for a Cell.
type CellImagesSpec struct {
	// +optional
	// +kubebuilder:validation:MinLength=1
	MultiGateway string `json:"multigateway,omitempty"`
	// +optional
	// +kubebuilder:validation:MinLength=1
	MultiOrch string `json:"multiorch,omitempty"`
}

// StatelessSpec defines the desired state for a scalable, stateless component
// like MultiAdmin, MultiOrch, or MultiGateway.
type StatelessSpec struct {
	// Replicas is the desired number of pods.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Affinity defines the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Resources defines the compute resource requirements.
	// +optional
	corev1.ResourceRequirements `json:"resources,omitempty"`
}

// GlobalTopoServerRefSpec defines a reference to the global topo server.
type GlobalTopoServerRefSpec struct {
	// RootPath is the root path being used in the global topo server.
	// +optional
	RootPath string `json:"rootPath,omitempty"`

	// ClientServiceName is the name of the etcd client service.
	// +kubebuilder:validation:MinLength=1
	// +optional
	ClientServiceName string `json:"clientServiceName,omitempty"`
}

// CellTopoServerSpec defines the topology server configuration for this cell.
// This is a one-of field; only one of Global, External, or ManagedSpec should be set.
// +kubebuilder:validation:XValidation:Rule="(has(self.global) ? 1 : 0) + (has(self.external) ? 1 : 0) + (has(self.managedSpec) ? 1 : 0) <= 1",Message="only one of 'global', 'external', or 'managedSpec' can be set for topoServer"
type CellTopoServerSpec struct {
	// Global indicates this cell uses the global topo server.
	// The reference details are populated by the parent controller.
	// +optional
	Global *GlobalTopoServerRefSpec `json:"global,omitempty"`

	// External defines connection details for an unmanaged, external topo server.
	// +optional
	External *ExternalTopoServerSpec `json:"external,omitempty"`

	// ManagedSpec defines the spec for a managed, cell-local topo server.
	// If set, the Cell controller will create a child TopoServer CR.
	// +optional
	ManagedSpec *TopoServerSpec `json:"managedSpec,omitempty"`
}

// TopologyReconciliationSpec defines flags for the cell controller.
type TopologyReconciliationSpec struct {
	// RegisterCell instructs the controller to register this cell in the topology.
	// +optional
	RegisterCell bool `json:"registerCell,omitempty"`

	// PruneTablets instructs the controller to prune old tablets from the topology.
	// +optional
	PruneTablets bool `json:"pruneTablets,omitempty"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// CellStatus defines the observed state of Cell
type CellStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Cell's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// GatewayReplicas is the current number of MultiGateway pods.
	// +optional
	GatewayReplicas int32 `json:"gatewayReplicas,omitempty"`

	// GatewayReadyReplicas is the number of MultiGateway pods ready to serve requests.
	// +optional
	GatewayReadyReplicas int32 `json:"gatewayReadyReplicas,omitempty"`

	// GatewayServiceName is the name of the MultiGateway service.
	// +optional
	GatewayServiceName string `json:"gatewayServiceName,omitempty"`

	// MultiOrchAvailable indicates whether the MultiOrch deployment is available.
	// +optional
	MultiOrchAvailable metav1.ConditionStatus `json:"multiorchAvailable,omitempty"`

	// TopoServerAvailable indicates whether the cell's topo server (local or global) is available.
	// +optional
	TopoServerAvailable metav1.ConditionStatus `json:"topoServerAvailable,omitempty"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status",description="Current availability status"
// +kubebuilder:printcolumn:name="Gateway Ready",type="string",JSONPath=".status.gatewayReadyReplicas",description="Gateway ready replicas"
// +kubebuilder:printcolumn:name="Gateway Total",type="string",JSONPath=".status.gatewayReplicas",description="Gateway total replicas"
// +kubebuilder:printcolumn:name="Orch Ready",type="string",JSONPath=".status.multiorchAvailable",description="Orchestrator status"
// +kubebuilder:printcolumn:name="Topo Ready",type="string",JSONPath=".status.topoServerAvailable",description="Topo server status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Cell is the Schema for the Cells API
type Cell struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CellSpec   `json:"spec,omitempty"`
	Status CellStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CellList contains a list of Cell
type CellList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cell `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cell{}, &CellList{})
}
