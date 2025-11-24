package shard

import (
	"fmt"

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
	poolName string,
	poolSpec multigresv1alpha1.ShardPoolSpec,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	name := buildPoolName(shard.Name, poolName)
	labels := buildPoolLabels(shard, poolName, poolSpec)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-headless",
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
func buildPoolName(shardName, poolName string) string {
	return fmt.Sprintf("%s-pool-%s", shardName, poolName)
}

// buildPoolLabels creates standard labels for pool resources, and uses the
// pool's database, table group, and cell details. Any additional labels are
// also merged, while keeping the main labels.
func buildPoolLabels(
	shard *multigresv1alpha1.Shard,
	poolName string,
	poolSpec multigresv1alpha1.ShardPoolSpec,
) map[string]string {
	fullPoolName := buildPoolName(shard.Name, poolName)
	cellName := poolSpec.Cell
	if cellName == "" {
		cellName = metadata.DefaultCellName
	}
	// TODO: Remove this once we figure what to do with the cell name.
	_ = cellName

	labels := metadata.BuildStandardLabels(fullPoolName, PoolComponentName)
	// TODO: Add multigres.com/* labels after finalizing label design:
	// metadata.AddCellLabel(labels, cellName)
	// metadata.AddDatabaseLabel(labels, poolSpec.Database)
	// metadata.AddTableGroupLabel(labels, poolSpec.TableGroup)

	metadata.MergeLabels(labels, shard.GetObjectMeta().GetLabels())

	return labels
}
