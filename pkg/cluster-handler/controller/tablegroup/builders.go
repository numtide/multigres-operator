package tablegroup

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/name"
)

// BuildShard constructs the desired Shard resource.
func BuildShard(
	tg *multigresv1alpha1.TableGroup,
	shardSpec *multigresv1alpha1.ShardResolvedSpec,
	scheme *runtime.Scheme,
) (*multigresv1alpha1.Shard, error) {
	// Build shard name from logical parts (cluster, database, tablegroup, shard)
	// NOT using tg.Name which includes the parent's hash
	clusterName := tg.Labels[metadata.LabelMultigresCluster]

	shardNameFull := name.JoinWithConstraints(
		name.DefaultConstraints,
		clusterName,
		string(tg.Spec.DatabaseName),
		string(tg.Spec.TableGroupName),
		shardSpec.Name,
	)

	labels := metadata.BuildStandardLabels(clusterName, metadata.ComponentShard)
	metadata.AddClusterLabel(labels, clusterName)
	metadata.AddDatabaseLabel(labels, tg.Spec.DatabaseName)
	metadata.AddTableGroupLabel(labels, tg.Spec.TableGroupName)
	metadata.AddShardLabel(labels, multigresv1alpha1.ShardName(shardSpec.Name))

	shardCR := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shardNameFull,
			Namespace: tg.Namespace,
			Labels:    labels,
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:     tg.Spec.DatabaseName,
			TableGroupName:   tg.Spec.TableGroupName,
			ShardName:        multigresv1alpha1.ShardName(shardSpec.Name),
			Images:           tg.Spec.Images,
			GlobalTopoServer: tg.Spec.GlobalTopoServer,
			MultiOrch:        shardSpec.MultiOrch,
			Pools:            shardSpec.Pools,
			// Merge hierarchy: Shard → TableGroup
			PVCDeletionPolicy: multigresv1alpha1.MergePVCDeletionPolicy(
				shardSpec.PVCDeletionPolicy,
				tg.Spec.PVCDeletionPolicy,
			),
		},
	}
	if err := controllerutil.SetControllerReference(tg, shardCR, scheme); err != nil {
		return nil, err
	}

	return shardCR, nil
}
