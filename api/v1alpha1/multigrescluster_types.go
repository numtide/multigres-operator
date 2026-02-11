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
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,resourceNames=multigres-operator-mutating-webhook-configuration,verbs=get;update;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,resourceNames=multigres-operator-validating-webhook-configuration,verbs=get;update;patch

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

	// MultiAdminWeb defines the configuration for the MultiAdminWeb component.
	// +optional
	MultiAdminWeb *MultiAdminWebConfig `json:"multiadminWeb,omitempty"`

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

	// PVCDeletionPolicy controls PVC lifecycle management for all stateful components.
	// Defaults to Retain/Retain for production safety.
	// +optional
	// +kubebuilder:default={whenDeleted: "Retain", whenScaled: "Retain"}
	PVCDeletionPolicy *PVCDeletionPolicy `json:"pvcDeletionPolicy,omitempty"`

	// Observability configures OpenTelemetry for data-plane components.
	// When nil, data-plane pods inherit the operator's own OTEL environment
	// variables at reconcile time. Set fields to override or disable.
	// +optional
	Observability *ObservabilityConfig `json:"observability,omitempty"`
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
	MultiGateway ImageRef `json:"multigateway,omitempty"`
	// +optional
	MultiOrch ImageRef `json:"multiorch,omitempty"`
	// +optional
	MultiPooler ImageRef `json:"multipooler,omitempty"`
	// +optional
	MultiAdmin ImageRef `json:"multiadmin,omitempty"`
	// +optional
	MultiAdminWeb ImageRef `json:"multiadminWeb,omitempty"`
	// +optional
	Postgres ImageRef `json:"postgres,omitempty"`
}

// ============================================================================
// Template Defaults Config Section Specs
// ============================================================================

// TemplateDefaults defines the default templates to use for components.
type TemplateDefaults struct {
	// CoreTemplate is the default template for global components.
	// +optional
	CoreTemplate TemplateRef `json:"coreTemplate,omitempty"`
	// CellTemplate is the default template for cells.
	// +optional
	CellTemplate TemplateRef `json:"cellTemplate,omitempty"`
	// ShardTemplate is the default template for shards.
	// +optional
	ShardTemplate TemplateRef `json:"shardTemplate,omitempty"`
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
	TemplateRef TemplateRef `json:"templateRef,omitempty"`
}

// MultiAdminWebConfig defines the configuration for MultiAdminWeb in the Cluster.
// It allows either an inline spec OR a reference to a CoreTemplate.
// +kubebuilder:validation:XValidation:rule="has(self.spec) || has(self.templateRef)",message="must specify either 'spec' or 'templateRef'"
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.templateRef))",message="cannot specify both 'spec' and 'templateRef'"
type MultiAdminWebConfig struct {
	// Spec defines the inline configuration.
	// +optional
	Spec *StatelessSpec `json:"spec,omitempty"`

	// TemplateRef refers to a CoreTemplate to load configuration from.
	// +optional
	TemplateRef TemplateRef `json:"templateRef,omitempty"`
}

// ============================================================================
// Cell Config Section Specs
// ============================================================================

// CellConfig defines a cell in the cluster.
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.cellTemplate))",message="cannot specify both 'spec' and 'cellTemplate'"
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.overrides))",message="cannot specify both 'spec' and 'overrides'"
// +kubebuilder:validation:XValidation:rule="has(self.zone) != has(self.region)",message="must specify either 'zone' or 'region', but not both"
type CellConfig struct {
	// Name is the logical name of the cell.
	Name CellName `json:"name"`

	// Zone indicates the physical availability zone.
	// +optional
	Zone Zone `json:"zone,omitempty"`
	// Region indicates the physical region (mutually exclusive with zone typically, but allowed here).
	// +optional
	Region Region `json:"region,omitempty"`

	// CellTemplate refers to a CellTemplate CR.
	// +optional
	CellTemplate TemplateRef `json:"cellTemplate,omitempty"`

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
	Name DatabaseName `json:"name"`

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
	Name TableGroupName `json:"name"`

	// Default indicates if this is the default/unsharded group.
	// +optional
	Default bool `json:"default,omitempty"`

	// Shards defines the list of shards.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Shards []ShardConfig `json:"shards,omitempty"`

	// PVCDeletionPolicy controls PVC lifecycle for shards in this table group.
	// Overrides MultigresCluster setting.
	// +optional
	PVCDeletionPolicy *PVCDeletionPolicy `json:"pvcDeletionPolicy,omitempty"`
}

// ShardConfig defines a specific shard.
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.shardTemplate))",message="cannot specify both 'spec' and 'shardTemplate'"
// +kubebuilder:validation:XValidation:rule="!(has(self.spec) && has(self.overrides))",message="cannot specify both 'spec' and 'overrides'"
type ShardConfig struct {
	// Name is the identifier of the shard (e.g., "0", "1").
	// +kubebuilder:validation:XValidation:rule="self == '0-inf'",message="shardName must be strictly equal to '0-inf' in this version"
	Name ShardName `json:"name"`

	// ShardTemplate refers to a ShardTemplate CR.
	// +optional
	ShardTemplate TemplateRef `json:"shardTemplate,omitempty"`

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
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) <= 25)",message="pool names must be <= 25 chars"
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(k, k in self)",message="Pools cannot be removed or renamed in this version (Append-Only)"
	Pools map[PoolName]PoolSpec `json:"pools,omitempty"`
}

// ShardInlineSpec defines the inline configuration for a Shard.
type ShardInlineSpec struct {
	// MultiOrch configuration.
	// +optional
	MultiOrch MultiOrchSpec `json:"multiorch,omitempty"`

	// Pools configuration. Keyed by pool name.
	// +optional
	// +kubebuilder:validation:MaxProperties=8
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) <= 25)",message="pool names must be <= 25 chars"
	// +kubebuilder:validation:XValidation:rule="oldSelf.all(k, k in self)",message="Pools cannot be removed or renamed in this version (Append-Only)"
	Pools map[PoolName]PoolSpec `json:"pools,omitempty"`

	// PVCDeletionPolicy controls PVC lifecycle for pools in this shard.
	// Overrides TableGroup and MultigresCluster settings.
	// +optional
	PVCDeletionPolicy *PVCDeletionPolicy `json:"pvcDeletionPolicy,omitempty"`
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
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the aggregated lifecycle state of the cluster.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// Message provides details about the current phase (e.g. error messages).
	// +optional
	Message string `json:"message,omitempty"`

	// Cells status summary.
	// +optional
	// +kubebuilder:validation:MaxProperties=50
	Cells map[CellName]CellStatusSummary `json:"cells,omitempty"`

	// Databases status summary.
	// +optional
	// +kubebuilder:validation:MaxProperties=50
	Databases map[DatabaseName]DatabaseStatusSummary `json:"databases,omitempty"`
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
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 25",message="MultigresCluster name must be at most 25 characters"
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
