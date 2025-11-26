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
// Cell Spec (Read-only API)
// ============================================================================
//
// Cell is a child CR managed by MultigresCluster.

// CellSpec defines the desired state of Cell.
type CellSpec struct {
	// Name is the logical name of the cell.
	Name string `json:"name"`
	// Zone indicates the physical availability zone.
	Zone string `json:"zone,omitempty"`

	// Images required for this cell.
	Images CellImages `json:"images"`

	// MultiGateway fully resolved config.
	MultiGateway StatelessSpec `json:"multiGateway"`

	// GlobalTopoServer reference (always populated).
	GlobalTopoServer GlobalTopoServerRef `json:"globalTopoServer"`

	// TopoServer defines the local topology config.
	// +optional
	TopoServer LocalTopoServerSpec `json:"topoServer,omitempty"`

	// AllCells list for discovery.
	// +optional
	AllCells []string `json:"allCells,omitempty"`

	// TopologyReconciliation flags.
	// +optional
	TopologyReconciliation TopologyReconciliation `json:"topologyReconciliation,omitempty"`
}

// CellImages defines the images required for a Cell.
type CellImages struct {
	MultiGateway string `json:"multigateway"`
}

// TopologyReconciliation defines flags for the cell controller.
type TopologyReconciliation struct {
	RegisterCell bool `json:"registerCell"`
	PruneTablets bool `json:"pruneTablets"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// CellStatus defines the observed state of Cell.
type CellStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	GatewayReplicas      int32  `json:"gatewayReplicas"`
	GatewayReadyReplicas int32  `json:"gatewayReadyReplicas"`
	GatewayServiceName   string `json:"gatewayServiceName,omitempty"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Gateway",type="integer",JSONPath=".status.gatewayReadyReplicas"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status"

// Cell is the Schema for the cells API
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
