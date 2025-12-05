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
// MultigresClusterSpec Spec (User-editable API)
// ============================================================================
//
// Defines the fields users interact with directly to declare their intent.
// Each section in this file corresponds to a block of configuration.
// Specs are fetched from their respective child CRs where feasible.
// The Spec order in this file matches the order in the following sample:
// plans/phase-1/multigres-operator-api-v1alpha1-design.md

// MultigresClusterSpec defines the desired state of MultigresCluster.
type MultigresClusterSpec struct {
	// Images defines the container images for all components in the cluster.
	// +optional
	Images ClusterImages `json:"images,omitempty"`

	// TemplateDefaults defines the default templates to use for components
	// that do not have explicit specs.
	// +optional
	TemplateDefaults TemplateDefaults `json:"templateDefaults,omitempty"`

	// GlobalTopoServer defines the cluster-wide global topology server.
	// +optional
	GlobalTopoServer GlobalTopoServerSpec `json:"globalTopoServer,omitempty"`

	// MultiAdmin defines the configuration for the MultiAdmin component.
	// +optional
	MultiAdmin ComponentConfig `json:"multiadmin,omitempty"`

	// Cells defines the list of cells (failure domains) in the cluster.
	// +optional
	Cells []CellConfig `json:"cells,omitempty"`

	// Databases defines the logical databases, table groups, and sharding.
	// +optional
	Databases []DatabaseConfig `json:"databases,omitempty"`
}

// ============================================================================
// Images Config Section Specs
// ============================================================================

// ClusterImages defines the container images for all components in the cluster.
type ClusterImages struct {
	// ImagePullPolicy overrides the default image pull policy.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets is a list of references to secrets in the same namespace.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Component Images
	// +optional
	MultiGateway string `json:"multigateway,omitempty"`
	// +optional
	MultiOrch string `json:"multiorch,omitempty"`
	// +optional
	MultiPooler string `json:"multipooler,omitempty"`
	// +optional
	MultiAdmin string `json:"multiadmin,omitempty"`
	// +optional
	Postgres string `json:"postgres,omitempty"`
}

// ============================================================================
// Template Defaults Config Section Specs
// ============================================================================

// TemplateDefaults defines the default templates to use for components.
type TemplateDefaults struct {
	// CoreTemplate is the default template for global components.
	// +optional
	CoreTemplate string `json:"coreTemplate,omitempty"`
	// CellTemplate is the default template for cells.
	// +optional
	CellTemplate string `json:"cellTemplate,omitempty"`
	// ShardTemplate is the default template for shards.
	// +optional
	ShardTemplate string `json:"shardTemplate,omitempty"`
}

// ============================================================================
// Cell Config Section Specs
// ============================================================================

// CellConfig defines a cell in the cluster.
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.cellTemplate))",message="cannot specify both 'spec' and 'cellTemplate'"
type CellConfig struct {
	// Name is the logical name of the cell.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Zone indicates the physical availability zone.
	// +optional
	Zone string `json:"zone,omitempty"`
	// Region indicates the physical region (mutually exclusive with zone typically, but allowed here).
	// +optional
	Region string `json:"region,omitempty"`

	// CellTemplate refers to a CellTemplate CR.
	// +optional
	CellTemplate string `json:"cellTemplate,omitempty"`

	// Overrides are applied on top of the template.
	// +optional
	Overrides *CellOverrides `json:"overrides,omitempty"`

	// Spec defines the inline configuration if no template is used.
	// +optional
	Spec *CellInlineSpec `json:"spec,omitempty"`
}

// CellOverrides defines overrides for a CellTemplate.
type CellOverrides struct {
	// MultiGateway overrides.
	// +optional
	MultiGateway *StatelessSpec `json:"multigateway,omitempty"`
}

// CellInlineSpec defines the inline configuration for a Cell.
type CellInlineSpec struct {
	// MultiGateway configuration.
	// +optional
	MultiGateway StatelessSpec `json:"multigateway,omitempty"`

	// LocalTopoServer configuration (optional).
	// +optional
	LocalTopoServer *LocalTopoServerSpec `json:"localTopoServer,omitempty"`
}

// ============================================================================
// Database Config Section Specs
// ============================================================================

// DatabaseConfig defines a logical database.
type DatabaseConfig struct {
	// Name is the logical name of the database.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Default indicates if this is the system default database.
	// +optional
	Default bool `json:"default,omitempty"`

	// TableGroups is a list of table groups.
	// +optional
	TableGroups []TableGroupConfig `json:"tablegroups,omitempty"`
}

// TableGroupConfig defines a table group within a database.
type TableGroupConfig struct {
	// Name is the logical name of the table group.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Default indicates if this is the default/unsharded group.
	// +optional
	Default bool `json:"default,omitempty"`

	// Shards defines the list of shards.
	// +optional
	Shards []ShardConfig `json:"shards,omitempty"`
}

// ShardConfig defines a specific shard.
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.shardTemplate))",message="cannot specify both 'spec' and 'shardTemplate'"
type ShardConfig struct {
	// Name is the identifier of the shard (e.g., "0", "1").
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// ShardTemplate refers to a ShardTemplate CR.
	// +optional
	ShardTemplate string `json:"shardTemplate,omitempty"`

	// Overrides are applied on top of the template.
	// +optional
	Overrides *ShardOverrides `json:"overrides,omitempty"`

	// Spec defines the inline configuration if no template is used.
	// +optional
	Spec *ShardInlineSpec `json:"spec,omitempty"`
}

// ShardOverrides defines overrides for a ShardTemplate.
type ShardOverrides struct {
	// MultiOrch overrides.
	// +optional
	MultiOrch *MultiOrchSpec `json:"multiorch,omitempty"`

	// Pools overrides. Keyed by pool name.
	// +optional
	Pools map[string]PoolSpec `json:"pools,omitempty"`
}

// ShardInlineSpec defines the inline configuration for a Shard.
type ShardInlineSpec struct {
	// MultiOrch configuration.
	// +optional
	MultiOrch MultiOrchSpec `json:"multiorch,omitempty"`

	// Pools configuration. Keyed by pool name.
	// +optional
	Pools map[string]PoolSpec `json:"pools,omitempty"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// MultigresClusterStatus defines the observed state.
type MultigresClusterStatus struct {
	// ObservedGeneration is the most recent generation observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Cells status summary (map of cell name to status).
	// +optional
	Cells map[string]CellStatusSummary `json:"cells,omitempty"`

	// Databases status summary.
	// +optional
	Databases map[string]DatabaseStatusSummary `json:"databases,omitempty"`
}

// CellStatusSummary provides a high-level status of a cell.
type CellStatusSummary struct {
	Ready           bool  `json:"ready"`
	GatewayReplicas int32 `json:"gatewayReplicas"`
}

// DatabaseStatusSummary provides a high-level status of a database.
type DatabaseStatusSummary struct {
	ReadyShards int32 `json:"readyShards"`
	TotalShards int32 `json:"totalShards"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MultigresCluster is the Schema for the multigresclusters API
type MultigresCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MultigresClusterSpec   `json:"spec,omitempty"`
	Status MultigresClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MultigresClusterList contains a list of MultigresCluster
type MultigresClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MultigresCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MultigresCluster{}, &MultigresClusterList{})
}
