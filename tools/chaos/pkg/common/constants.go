package common

import "time"

// Label keys used by the multigres operator on managed resources.
const (
	LabelAppManagedBy        = "app.kubernetes.io/managed-by"
	LabelAppComponent        = "app.kubernetes.io/component"
	LabelAppInstance         = "app.kubernetes.io/instance"
	LabelMultigresCluster    = "multigres.com/cluster"
	LabelMultigresCell       = "multigres.com/cell"
	LabelMultigresShard      = "multigres.com/shard"
	LabelMultigresPool       = "multigres.com/pool"
	LabelMultigresDatabase   = "multigres.com/database"
	LabelMultigresTableGroup = "multigres.com/tablegroup"

	ManagedByMultigres = "multigres-operator"
)

// Annotation keys used by the multigres operator.
const (
	AnnotationSpecHash         = "multigres.com/spec-hash"
	AnnotationDrainState       = "drain.multigres.com/state"
	AnnotationDrainRequestedAt = "drain.multigres.com/requested-at"
)

// Drain states matching the operator's state machine.
const (
	DrainStateRequested        = "requested"
	DrainStateDraining         = "draining"
	DrainStateAcknowledged     = "acknowledged"
	DrainStateReadyForDeletion = "ready-for-deletion"
)

// Component names for app.kubernetes.io/component labels.
const (
	ComponentPool         = "shard-pool"
	ComponentMultiOrch    = "multiorch"
	ComponentMultiGateway = "multigateway"
	ComponentGlobalTopo   = "global-topo"
)

// Well-known ports for multigres components.
const (
	PortMultiGatewayHTTP = 15100
	PortMultiGatewayGRPC = 15170
	PortMultiGatewayPG   = 15432
	PortMultiPoolerHTTP  = 15200
	PortMultiOrchHTTP    = 15300
	PortMultiOrchGRPC    = 15370
	PortEtcdClient       = 2379
	PortEtcdPeer         = 2380
	PortOperatorHealth   = 8081
	PortPostgres         = 5432
)

// PostgreSQL NAMEDATALEN limit for application_name.
const PGNameDataLen = 63

// Default thresholds for observer checks.
const (
	DefaultInterval              = 10 * time.Second
	PendingTimeout               = 60 * time.Second
	NotReadyTimeout              = 30 * time.Second
	TerminatingTimeout           = 90 * time.Second
	RestartWarnThreshold         = 1
	RestartErrorThreshold        = 3
	RestartErrorWindow           = 5 * time.Minute
	DrainRequestedTimeout        = 30 * time.Second
	DrainDrainingTimeout         = 5 * time.Minute
	DrainAcknowledgedTimeout     = 30 * time.Second
	PhaseDegradedTimeout         = 5 * time.Minute
	GenerationDivergeTimeout     = 60 * time.Second
	PrimaryGracePeriod           = 30 * time.Second
	StaleStatusEntryGracePeriod  = 30 * time.Second
	TerminatingResourceTimeout   = 5 * time.Minute
	ConnectivityTimeout          = 5 * time.Second
	ConnectivityLatencyThreshold = 500 * time.Millisecond
	ReplicationLagWarnSecs       = 10
	ReplicationLagErrorSecs      = 60
)

// Operator event reasons to monitor.
const (
	EventReasonBackupStale      = "BackupStale"
	EventReasonConfigError      = "ConfigError"
	EventReasonExpandPVCFailed  = "ExpandPVCFailed"
	EventReasonPodReplaced      = "PodReplaced"
	EventReasonStatusError      = "StatusError"
	EventReasonStuckTerminating = "StuckTerminating"
	EventReasonTopologyError    = "TopologyError"
)
