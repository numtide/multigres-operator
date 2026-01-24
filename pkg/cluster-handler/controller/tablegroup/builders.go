package tablegroup

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
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
	shardNameFull := name.JoinWithConstraints(
		name.DefaultConstraints,
		tg.Labels["multigres.com/cluster"],
		tg.Spec.DatabaseName,
		tg.Spec.TableGroupName,
		shardSpec.Name,
	)

	shardCR := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shardNameFull,
			Namespace: tg.Namespace,
			Labels: map[string]string{
				"multigres.com/cluster":    tg.Labels["multigres.com/cluster"],
				"multigres.com/database":   tg.Spec.DatabaseName,
				"multigres.com/tablegroup": tg.Spec.TableGroupName,
				"multigres.com/shard":      shardSpec.Name,
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:     tg.Spec.DatabaseName,
			TableGroupName:   tg.Spec.TableGroupName,
			ShardName:        shardSpec.Name,
			Images:           tg.Spec.Images,
			GlobalTopoServer: tg.Spec.GlobalTopoServer,
			MultiOrch:        shardSpec.MultiOrch,
			Pools:            shardSpec.Pools,
		},
	}

	if err := controllerutil.SetControllerReference(tg, shardCR, scheme); err != nil {
		return nil, err
	}

	return shardCR, nil
}
