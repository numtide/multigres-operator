package shard

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
)

const (
	// PoolComponentName is the component label value for pool resources
	PoolComponentName = "shard-pool"
)

// BuildPoolHeadlessService creates a headless Service for a pool's StatefulSet in a specific cell.
// Headless services are required for StatefulSet pod DNS records.
func BuildPoolHeadlessService(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	// IMPORTANT: Use safe hashing for the headless service name too, or we risk exceeding 63 chars
	// by appending "-headless" to an already hashed/truncated name.
	// Logic: Use LOGICAL parts from Spec/Labels to avoid chaining hashes.
	headlessName := buildPoolHeadlessServiceName(shard, poolName, cellName)
	labels := buildPoolLabelsWithCell(shard, poolName, cellName, poolSpec)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 labels,
			Ports:                    buildPoolHeadlessServicePorts(),
			PublishNotReadyAddresses: true,
		},
	}

	if err := ctrl.SetControllerReference(shard, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}

// buildPoolNameWithCell generates the name for pool resources in a specific cell.
// Format: {shardName}-pool-{poolName}-{cellName}
// For pools spanning multiple cells, this creates unique names per cell.
// cellName must not be empty - pools must belong to a cell.
// buildPoolNameWithCell generates the name for pool resources in a specific cell.
// Format: {cluster}-{db}-{tg}-{shard}-pool-{poolName}-{cellName}
func buildPoolNameWithCell(shard *multigresv1alpha1.Shard, poolName, cellName string) string {
	// Logic: Use LOGICAL parts from Spec/Labels to avoid double hashing.
	// shard.Name is already hashed (cluster-db-tg-shard-HASH).
	clusterName := shard.Labels["multigres.com/cluster"]
	return names.JoinWithConstraints(
		names.StatefulSetConstraints,
		clusterName,
		shard.Spec.DatabaseName,
		shard.Spec.TableGroupName,
		shard.Spec.ShardName,
		"pool",
		poolName,
		cellName,
	)
}

// buildPoolHeadlessServiceName generates the name for pool headless service in a specific cell.
func buildPoolHeadlessServiceName(shard *multigresv1alpha1.Shard, poolName, cellName string) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	return names.JoinWithConstraints(
		names.ServiceConstraints,
		clusterName,
		shard.Spec.DatabaseName,
		shard.Spec.TableGroupName,
		shard.Spec.ShardName,
		"pool",
		poolName,
		cellName,
		"headless",
	)
}
