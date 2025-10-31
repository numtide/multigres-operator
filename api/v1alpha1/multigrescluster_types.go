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
// Each section in this file corresponds to a block of configuration
// Specs are fetched from their respective child CRs where feasible.
// The Spec order in this file matches the order in the following sample:
// plans/phase-1/multigres-operator-api-v1alpha1-design.md

// MultigresClusterSpec defines the desired state of MultigresCluster
type MultigresClusterSpec struct {
	// Images defines the container images for all components in the cluster.
	// +optional
	Images *ImagesTemplateSpec `json:"images,omitempty"`

	// GlobalTopoServer defines the cluster-wide global topology server.
	// +optional
	GlobalTopoServer *GlobalTopoServerConfig `json:"globalTopoServer,omitempty"`

	// MultiAdmin defines the deployment for the cluster-wide admin component.
	// +optional
	MultiAdmin *MultiAdminConfig `json:"multiadmin,omitempty"`

	// Cells defines the different failure domains or regions for the cluster.
	// +optional
	Cells *CellsConfig `json:"cells,omitempty"`

	// Databases defines the logical databases, table groups, and sharding.
	// +optional
	Databases *DatabasesConfig `json:"databases,omitempty"`
}

// ============================================================================
// Images Config Section Specs
// ============================================================================

// ImagesTemplateSpec defines all images for the cluster.
type ImagesTemplateSpec struct {
	CommonImagesSpec `json:",inline"`

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

// CommonImagesSpec holds container image pull policy and secrets.
type CommonImagesSpec struct {
	// ImagePullPolicy overrides the default image pull policy.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets is an optional list of references to secrets in the same namespace
	// to use for pulling images.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// ============================================================================
// TopoServer Config Section Specs
// ============================================================================

// GlobalTopoServerConfig defines the configuration for the global topo server.
// Either deploymentTemplate or managedSpec is allowed. Not both.
// Overrides is only allowed when DeploymentTemplate is provided.
// external and deploymentSpec (or deploymentTemplate) cannot be used at the same time.
// NOTE: the validation code could be simpler if we use field nesting.
// +kubebuilder:validation:XValidation:rule="!((has(self.deploymentTemplate) && self.deploymentTemplate != \"\") && has(self.managedSpec))",message="deploymentTemplate and managedSpec are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && (!has(self.deploymentTemplate) || self.deploymentTemplate == \"\"))",message="overrides can only be set if deploymentTemplate is also set"
// +kubebuilder:validation:XValidation:rule="!(has(self.external) && ((has(self.deploymentTemplate) && self.deploymentTemplate != \"\") || has(self.managedSpec)))",message="external and managed topo server (deploymentTemplate or managedSpec) are mutually exclusive"
type GlobalTopoServerConfig struct {
	// RootPath is the root path to use within the etcd cluster.
	// +optional
	RootPath string `json:"rootPath,omitempty"`

	// DeploymentTemplate is the name of a DeploymentTemplate
	// to load the `managedTopoServer` spec from.
	// +optional
	DeploymentTemplate string `json:"deploymentTemplate,omitempty"`

	// Overrides are applied on top of the loaded template spec.
	// +optional
	Overrides *TopoServerSpec `json:"overrides,omitempty"`

	// ManagedSpec defines an inline spec for a managed global topo server.
	// This is used if DeploymentTemplate is not specified.
	// +optional
	ManagedSpec *TopoServerSpec `json:"managedSpec,omitempty"`

	// External defines connection details for an unmanaged, external topo server.
	// If this is set, no TopoServer CR will be created.
	// +optional
	External *ExternalTopoServerSpec `json:"external,omitempty"`
}

// ExternalTopoServerSpec defines the connection details for an unmanaged, external topo server.
type ExternalTopoServerSpec struct {
	// Address is the client URL of the external etcd cluster (e.g., "my-etcd.svc:2379").
	// +kubebuilder:validation:MinLength:=1
	Address string `json:"address"`

	// RootPath is the etcd root path for this topo server.
	// +optional
	RootPath string `json:"rootPath,omitempty"`
}

// ============================================================================
// MultiAdmin Config Section Specs
// ============================================================================

// MultiAdminConfig defines the configuration for the MultiAdmin component.
// Either DeploymentTemplate or inline fields is allowed. Not both.
// Overrides is only allowed when DeploymentTemplate is provided.
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && (!has(self.deploymentTemplate) || self.deploymentTemplate == \"\"))",message="overrides can only be set if deploymentTemplate is also set"
type MultiAdminConfig struct {
	// DeploymentTemplate is the name of a DeploymentTemplate
	// to load the `multiadmin` spec from.
	// +optional
	DeploymentTemplate string `json:"deploymentTemplate,omitempty"`

	// Overrides are applied on top of the loaded template spec.
	// +optional
	Overrides *StatelessSpec `json:"overrides,omitempty"`

	// Inline spec, used if DeploymentTemplate is not specified.
	StatelessSpec `json:",inline"`
}

// ============================================================================
// Cell Config Section Specs
// ============================================================================

// CellsConfig holds the list of cell templates.
type CellsConfig struct {
	// Templates is a list of cell definitions.
	// +optional
	Templates []CellTemplate `json:"templates,omitempty"`
}

// CellTemplate defines a named cell configuration.
type CellTemplate struct {
	// Name is the logical name of the cell (e.g., "us-east-1").
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=63
	// +kubebuilder:validation:Pattern:="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Name string `json:"name"`
	// Spec is the configuration for this cell.
	Spec CellSpecConfig `json:"spec"`
}

// CellSpecConfig defines the configuration for a cell.
type CellSpecConfig struct {
	// +optional
	MultiGateway *MultiGatewayConfig `json:"multigateway,omitempty"`
	// +optional
	MultiOrch *MultiOrchConfig `json:"multiorch,omitempty"`
	// +optional
	TopoServer *CellTopoServerConfig `json:"topoServer,omitempty"`
}

// MultiGatewayConfig defines the configuration for a cell's MultiGateway.
// Either DeploymentTemplate or inline fields is allowed. Not both.
// Overrides is only allowed when DeploymentTemplate is provided.
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && (!has(self.deploymentTemplate) || self.deploymentTemplate == \"\"))",message="overrides can only be set if deploymentTemplate is also set"
type MultiGatewayConfig struct {
	// DeploymentTemplate is the name of a DeploymentTemplate
	// to load the `multigateway` spec from.
	// +optional
	DeploymentTemplate string `json:"deploymentTemplate,omitempty"`

	// Overrides are applied on top of the loaded template spec.
	// +optional
	Overrides *StatelessSpec `json:"overrides,omitempty"`

	// Inline spec, used if DeploymentTemplate is not specified.
	StatelessSpec `json:",inline"`
}

// MultiOrchConfig defines the configuration for a cell's MultiOrch.
// Either DeploymentTemplate or inline fields is allowed. Not both.
// Overrides is only allowed when DeploymentTemplate is provided.
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && (!has(self.deploymentTemplate) || self.deploymentTemplate == \"\"))",message="overrides can only be set if deploymentTemplate is also set"
type MultiOrchConfig struct {
	// DeploymentTemplate is the name of a DeploymentTemplate
	// to load the `multiorch` spec from.
	// +optional
	DeploymentTemplate string `json:"deploymentTemplate,omitempty"`

	// Overrides are applied on top of the loaded template spec.
	// +optional
	Overrides *StatelessSpec `json:"overrides,omitempty"`

	// Inline spec, used if DeploymentTemplate is not specified.
	StatelessSpec `json:",inline"`
}

// CellTopoServerConfig defines the topo server config for a cell.
// Either deploymentTemplate or managedSpec is allowed. Not both.
// Overrides is only allowed when DeploymentTemplate is provided.
// external and deploymentSpec (or deploymentTemplate) cannot be used at the same time.
// NOTE: the validation code could be simpler if we use field nesting.
// +kubebuilder:validation:XValidation:rule="!((has(self.deploymentTemplate) && self.deploymentTemplate != \"\") && has(self.managedSpec))",message="deploymentTemplate and managedSpec are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.external) && ((has(self.deploymentTemplate) && self.deploymentTemplate != \"\") || has(self.managedSpec)))",message="external and managed topo server (deploymentTemplate or managedSpec) are mutually exclusive"
type CellTopoServerConfig struct {
	// RootPath is the root path to use within the etcd cluster.
	// +optional
	RootPath string `json:"rootPath,omitempty"`

	// DeploymentTemplate is the name of a DeploymentTemplate
	// to load the `managedTopoServer` spec from.
	// +optional
	DeploymentTemplate string `json:"deploymentTemplate,omitempty"`

	// ManagedSpec defines an inline spec for a managed local topo server.
	// +optional
	ManagedSpec *TopoServerSpec `json:"managedSpec,omitempty"`

	// External defines connection details for an unmanaged, external topo server.
	// +optional
	External *ExternalTopoServerSpec `json:"external,omitempty"`
	// Note: If all fields are nil, the cell defaults to using the GlobalTopoServer.
}

// ============================================================================
// Databases (tablepools and shards) Config Section Specs
// ============================================================================

// DatabasesConfig holds the list of database templates.
type DatabasesConfig struct {
	// Templates is a list of database definitions.
	// +optional
	Templates []DatabaseTemplate `json:"templates,omitempty"`
}

// DatabaseTemplate defines a named database configuration.
type DatabaseTemplate struct {
	// Name is the logical name of the database (e.g., "production_db").
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:Pattern:="^[a-zA-Z_][a-zA-Z0-9_]*$"
	Name string `json:"name"`
	// Spec is the configuration for this database.
	Spec DatabaseSpecConfig `json:"spec"`
}

// DatabaseSpecConfig defines the configuration for a logical database.
type DatabaseSpecConfig struct {
	// TableGroups is a list of table group definitions for this database.
	// +optional
	TableGroups []TableGroupConfig `json:"tablegroups,omitempty"`
}

// TableGroupConfig defines the configuration for a table group.
type TableGroupConfig struct {
	// Name is the logical name of the table group (e.g., "default", "orders_tg").
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:Pattern:="^[a-zA-Z_][a-zA-Z0-9_]*$"
	Name string `json:"name"`

	// Partitioning defines how this table group is sharded.
	Partitioning PartitioningSpec `json:"partitioning"`

	// ShardTemplate defines the configuration for the shards in this group.
	ShardTemplate ShardTemplateConfig `json:"shardTemplate"`
}

// ShardTemplateConfig defines the template for shards in a table group.
type ShardTemplateConfig struct {
	// Pools is a list of pool configurations for each shard.
	// +optional
	Pools []ShardPoolConfig `json:"pools,omitempty"`
}

// ShardPoolConfig defines the configuration for a shard pool,
// supporting templates, overrides, and inline definitions.
// Either DeploymentTemplate or inline fields is allowed. Not both.
// Overrides is only allowed when DeploymentTemplate is provided.
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && (!has(self.deploymentTemplate) || self.deploymentTemplate == \"\"))",message="overrides can only be set if deploymentTemplate is also set"
type ShardPoolConfig struct {
	// DeploymentTemplate is the name of a DeploymentTemplate
	// to load the `shardPool` spec from.
	// +optional
	DeploymentTemplate string `json:"deploymentTemplate,omitempty"`

	// Overrides are applied on top of the loaded template spec.
	// +optional
	Overrides *ShardPoolSpec `json:"overrides,omitempty"`

	// Inline spec, used if DeploymentTemplate is not specified.
	ShardPoolSpec `json:",inline"`
}

// ============================================================================
// CR Controller Status Specs
// ============================================================================

// Condition constants. NOTE: these may go somewhere else

const (
	ConditionAvailable = "Available"

	ConditionProgressing = "Progressing"
)

// MultigresClusterStatus defines the observed state of MultigresCluster
type MultigresClusterStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the cluster's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// GlobalTopoServer provides a high-level status of the global topo server.
	// +optional
	GlobalTopoServer ComponentStatus `json:"globalTopoServer,omitempty"`

	// MultiAdmin provides a high-level status of the MultiAdmin component.
	// +optional
	MultiAdmin ComponentStatus `json:"multiadmin,omitempty"`

	// Cells provides a map of cell names to their high-level status.
	// +optional
	Cells map[string]CellStatusSummary `json:"cells,omitempty"`

	// Databases provides a map of database names to their high-level status.
	// +optional
	Databases map[string]DatabaseStatusSummary `json:"databases,omitempty"`
}

// ComponentStatus indicates the simple availability of a component.
type ComponentStatus struct {
	// Available indicates whether the component is ready.
	// +optional
	Available metav1.ConditionStatus `json:"available,omitempty"`
	// ServiceName is the name of the Kubernetes service for this component.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`
}

// CellStatusSummary provides a high-level status of a cell.
type CellStatusSummary struct {
	// +optional
	GatewayAvailable metav1.ConditionStatus `json:"gatewayAvailable,omitempty"`
	// +optional
	MultiOrchAvailable metav1.ConditionStatus `json:"multiorchAvailable,omitempty"`
	// +optional
	TopoServerAvailable metav1.ConditionStatus `json:"topoServerAvailable,omitempty"`
}

// DatabaseStatusSummary provides a high-level status of a database.
type DatabaseStatusSummary struct {
	// DesiredInstances is the total number of shard pods desired across all table groups.
	// +optional
	DesiredInstances int32 `json:"desiredInstances,omitempty"`
	// ReadyInstances is the number of shard pods that are ready.
	// +optional
	ReadyInstances int32 `json:"readyInstances,omitempty"`
	// ServingWrites indicates whether the database is capable of serving write traffic.
	// +optional
	ServingWrites metav1.ConditionStatus `json:"servingWrites,omitempty"`
}

// ============================================================================
// Kind Definition and registration
// ============================================================================

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status",description="Current cluster availability"
// +kubebuilder:printcolumn:name="Progressing",type="string",JSONPath=".status.conditions[?(@.type=='Progressing')].status",description="Is the cluster reconciling"
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
