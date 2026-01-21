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
// RBAC Markers
// ============================================================================

// -- Standard CRD Permissions --
// +kubebuilder:rbac:groups=multigres.com,resources=multigresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=multigresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=multigresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=multigres.com,resources=coretemplates;celltemplates;shardtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=multigres.com,resources=cells;tablegroups;toposervers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// -- Certificate Manager Permissions (ADDED) --
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;update;patch

// ============================================================================
// MultigresClusterSpec Spec (User-editable API)
// ============================================================================

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
	GlobalTopoServer *GlobalTopoServerSpec `json:"globalTopoServer,omitempty"`

	// MultiAdmin defines the configuration for the MultiAdmin component.
	// +optional
	MultiAdmin *MultiAdminConfig `json:"multiadmin,omitempty"`

	// Cells defines the list of cells (failure domains) in the cluster.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=50
	Cells []CellConfig `json:"cells,omitempty"`

	// Databases defines the logical databases, table groups, and sharding.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:XValidation:rule="self.all(db, db.name == 'postgres' && db.default == true)",message="in v1alpha1, only the single system database named 'postgres' (marked default: true) is supported"
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
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	MultiGateway string `json:"multigateway,omitempty"`
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	MultiOrch string `json:"multiorch,omitempty"`
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	MultiPooler string `json:"multipooler,omitempty"`
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	MultiAdmin string `json:"multiadmin,omitempty"`
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Postgres string `json:"postgres,omitempty"`
}

// ============================================================================
// Template Defaults Config Section Specs
// ============================================================================

// TemplateDefaults defines the default templates to use for components.
type TemplateDefaults struct {
	// CoreTemplate is the default template for global components.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	CoreTemplate string `json:"coreTemplate,omitempty"`
	// CellTemplate is the default template for cells.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	CellTemplate string `json:"cellTemplate,omitempty"`
	// ShardTemplate is the default template for shards.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	ShardTemplate string `json:"shardTemplate,omitempty"`
}

// ============================================================================
// MultiAdmin Config Section Specs
// ============================================================================

// MultiAdminConfig defines the configuration for MultiAdmin in the Cluster.
// It allows either an inline spec OR a reference to a CoreTemplate.
// +kubebuilder:validation:XValidation:rule="has(self.spec) || has(self.templateRef)",message="must specify either 'spec' or 'templateRef'"
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.templateRef))",message="cannot specify both 'spec' and 'templateRef'"
type MultiAdminConfig struct {
	// Spec defines the inline configuration.
	// +optional
	Spec *StatelessSpec `json:"spec,omitempty"`

	// TemplateRef refers to a CoreTemplate to load configuration from.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	TemplateRef string `json:"templateRef,omitempty"`
}

// ============================================================================
// Cell Config Section Specs
// ============================================================================

// CellConfig defines a cell in the cluster.
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.cellTemplate))",message="cannot specify both 'spec' and 'cellTemplate'"
// +kubebuilder:validation:XValidation:rule="has(self.zone) != has(self.region)",message="must specify either 'zone' or 'region', but not both"
type CellConfig struct {
	// Name is the logical name of the cell.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Zone indicates the physical availability zone.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Zone string `json:"zone,omitempty"`
	// Region indicates the physical region (mutually exclusive with zone typically, but allowed here).
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Region string `json:"region,omitempty"`

	// CellTemplate refers to a CellTemplate CR.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
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
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Default indicates if this is the system default database.
	// +optional
	Default bool `json:"default,omitempty"`

	// TableGroups is a list of table groups.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:XValidation:rule="self.filter(x, has(x.default) && x.default).size() == 1",message="every database must have exactly one tablegroup marked as default"
	// +kubebuilder:validation:MaxItems=20
	TableGroups []TableGroupConfig `json:"tablegroups,omitempty"`
}

// TableGroupConfig defines a table group within a database.
// +kubebuilder:validation:XValidation:rule="!has(self.default) || !self.default || self.name == 'default'",message="the default tablegroup must be named 'default'"
type TableGroupConfig struct {
	// Name is the logical name of the table group.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Default indicates if this is the default/unsharded group.
	// +optional
	Default bool `json:"default,omitempty"`

	// Shards defines the list of shards.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Shards []ShardConfig `json:"shards,omitempty"`
}

// ShardConfig defines a specific shard.
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.shardTemplate))",message="cannot specify both 'spec' and 'shardTemplate'"
type ShardConfig struct {
	// Name is the identifier of the shard (e.g., "0", "1").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// ShardTemplate refers to a ShardTemplate CR.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
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
	// +kubebuilder:validation:MaxProperties=8
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) < 63)",message="pool names must be < 63 chars"
	Pools map[string]PoolSpec `json:"pools,omitempty"`
}

// ShardInlineSpec defines the inline configuration for a Shard.
type ShardInlineSpec struct {
	// MultiOrch configuration.
	// +optional
	MultiOrch MultiOrchSpec `json:"multiorch,omitempty"`

	// Pools configuration. Keyed by pool name.
	// +optional
	// +kubebuilder:validation:MaxProperties=8
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) < 63)",message="pool names must be < 63 chars"
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
	// Cells status summary.
	// +optional
	// +kubebuilder:validation:MaxProperties=50
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) < 63)",message="cell names must be < 63 chars"
	Cells map[string]CellStatusSummary `json:"cells,omitempty"`

	// Databases status summary.
	// +optional
	// +kubebuilder:validation:MaxProperties=50
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) < 63)",message="database names must be < 63 chars"
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
// +kubebuilder:resource:shortName=mgc
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
