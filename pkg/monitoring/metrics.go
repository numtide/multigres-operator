package monitoring

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Domain-specific metric collectors.
//
// These complement the generic controller-runtime metrics (reconcile counts,
// durations, work queue depth, etc.) with operator-specific state that the
// framework cannot know about.
var (
	clusterInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multigres_operator_cluster_info",
			Help: "Info-style metric for MultigresCluster discovery and phase tracking. Always 1.",
		},
		[]string{"name", "namespace", "phase"},
	)

	clusterCellsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multigres_operator_cluster_cells_total",
			Help: "Number of cells in a MultigresCluster.",
		},
		[]string{"cluster", "namespace"},
	)

	clusterShardsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multigres_operator_cluster_shards_total",
			Help: "Number of shards in a MultigresCluster.",
		},
		[]string{"cluster", "namespace"},
	)

	cellGatewayReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multigres_operator_cell_gateway_replicas",
			Help: "MultiGateway replica counts for a Cell.",
		},
		[]string{"cell", "namespace", "state"},
	)

	shardPoolReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multigres_operator_shard_pool_replicas",
			Help: "Pool replica counts for a Shard.",
		},
		[]string{"shard", "pool", "namespace", "state"},
	)

	toposerverReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multigres_operator_toposerver_replicas",
			Help: "TopoServer replica counts.",
		},
		[]string{"name", "namespace", "state"},
	)

	webhookRequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "multigres_operator_webhook_request_total",
			Help: "Total number of webhook admission requests.",
		},
		[]string{"operation", "resource", "result"},
	)

	webhookRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "multigres_operator_webhook_request_duration_seconds",
			Help:    "Latency of webhook admission handling in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "resource"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		clusterInfo,
		clusterCellsTotal,
		clusterShardsTotal,
		cellGatewayReplicas,
		shardPoolReplicas,
		toposerverReplicas,
		webhookRequestTotal,
		webhookRequestDuration,
	)
}

// Collectors returns all registered metric collectors. This is useful for
// testing that metrics are properly registered.
func Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		clusterInfo,
		clusterCellsTotal,
		clusterShardsTotal,
		cellGatewayReplicas,
		shardPoolReplicas,
		toposerverReplicas,
		webhookRequestTotal,
		webhookRequestDuration,
	}
}
