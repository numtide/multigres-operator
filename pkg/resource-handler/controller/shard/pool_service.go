package shard

import (
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

const (
	// PoolComponentName is the component label value for pool resources
	PoolComponentName = "shard-pool"
)

// BuildPoolHeadlessService creates a headless Service for a pool's StatefulSet.
// Headless services are required for StatefulSet pod DNS records.
func BuildPoolHeadlessService(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.ShardPoolSpec,
	poolIndex int,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	poolName := buildPoolName(shard.Name, pool, poolIndex)
	labels := buildPoolLabels(shard, pool, poolName)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolName + "-headless",
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

// buildPoolName generates a consistent name for pool resources.
func buildPoolName(shardName string, poolName string, poolSpec multigresv1alpha1.ShardPoolSpec) string {
	return fmt.Sprintf("%s-pool-%s", shardName, poolName)
}

// buildPoolLabels creates standard labels for pool resources.
// Uses the pool's cell name from the Cell field.
func buildPoolLabels(
	shard *multigresv1alpha1.Shard,
	pool multigresv1alpha1.ShardPoolSpec,
	poolName string,
) map[string]string {
	cellName := pool.Cell
	if cellName == "" {
		cellName = metadata.DefaultCellName
	}

	labels := metadata.BuildStandardLabels(poolName, PoolComponentName)
	// Merge any labels associated from Shard.
	maps.Copy(labels, shard.GetObjectMeta().GetLabels())

	return labels
}
