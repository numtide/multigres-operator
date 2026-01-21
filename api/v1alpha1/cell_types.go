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
// (pkg/resource-handler/controller/cell/cell_controller.go)
// to follow kubebuilder conventions. They are temporarily placed here because
// controller-gen cannot process files in go.work modules.
//
// +kubebuilder:rbac:groups=multigres.com,resources=cells,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=cells/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=cells/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// ============================================================================
// Cell Spec (Read-only API)
// ============================================================================
//
// Cell is a child CR managed by MultigresCluster.

// CellName is a string restricted to 63 characters for strict validation budgeting.
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
type CellName string

// CellSpec defines the desired state of Cell.
// +kubebuilder:validation:XValidation:rule="has(self.zone) != has(self.region)",message="must specify either 'zone' or 'region', but not both"
type CellSpec struct {
	// Name is the logical name of the cell.
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`
	// Zone indicates the physical availability zone.
	// +kubebuilder:validation:MaxLength=63
	Zone string `json:"zone,omitempty"`
	// Region indicates the physical region.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Region string `json:"region,omitempty"`

	// Images defines the container images used in this cell.
	Images CellImages `json:"images"`

	// MultiGateway fully resolved config.
	MultiGateway StatelessSpec `json:"multigateway"`

	// GlobalTopoServer reference (always populated).
	GlobalTopoServer GlobalTopoServerRef `json:"globalTopoServer"`

	// TopoServer defines the local topology config.
	// +optional
	TopoServer *LocalTopoServerSpec `json:"topoServer,omitempty"`

	// AllCells list for discovery.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	AllCells []CellName `json:"allCells,omitempty"`

	// TopologyReconciliation flags.
	// +optional
	TopologyReconciliation TopologyReconciliation `json:"topologyReconciliation,omitempty"`
}

// CellImages defines the images required for a Cell.
type CellImages struct {
	// ImagePullPolicy overrides the default image pull policy.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets is a list of references to secrets in the same namespace.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// MultiGateway is the image used for the gateway.
	// +kubebuilder:validation:MaxLength=512
	MultiGateway string `json:"multigateway"`
}

// TopologyReconciliation defines flags for the cell controller.
type TopologyReconciliation struct {
	// RegisterCell indicates if the cell should register itself in the topology.
	RegisterCell bool `json:"registerCell"`

	// PrunePoolers indicates if unused poolers should be removed.
	PrunePoolers bool `json:"prunePoolers"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// CellStatus defines the observed state of Cell.
type CellStatus struct {
	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// GatewayReplicas is the total number of gateway pods.
	GatewayReplicas int32 `json:"gatewayReplicas"`

	// GatewayReadyReplicas is the number of gateway pods that are ready.
	GatewayReadyReplicas int32 `json:"gatewayReadyReplicas"`

	// GatewayServiceName is the DNS name of the gateway service.
	// +kubebuilder:validation:MaxLength=253
	GatewayServiceName string `json:"gatewayServiceName,omitempty"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Gateway",type="integer",JSONPath=".status.gatewayReadyReplicas"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status"

// Cell is the Schema for the cells API
// +kubebuilder:resource:shortName=cel
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
