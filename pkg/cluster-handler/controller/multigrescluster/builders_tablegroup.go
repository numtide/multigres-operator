package multigrescluster

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/name"
)

// BuildTableGroup constructs the desired TableGroup resource.
func BuildTableGroup(
	cluster *multigresv1alpha1.MultigresCluster,
	dbName multigresv1alpha1.DatabaseName,
	tgCfg *multigresv1alpha1.TableGroupConfig,
	resolvedShards []multigresv1alpha1.ShardResolvedSpec,
	globalTopoRef multigresv1alpha1.GlobalTopoServerRef,
	scheme *runtime.Scheme,
) (*multigresv1alpha1.TableGroup, error) {
	tgNameHash := name.JoinWithConstraints(
		name.DefaultConstraints,
		cluster.Name,
		string(dbName),
		string(tgCfg.Name),
	)

	labels := metadata.BuildStandardLabels(cluster.Name, metadata.ComponentTableGroup)
	metadata.AddClusterLabel(labels, cluster.Name)
	metadata.AddDatabaseLabel(labels, dbName)
	metadata.AddTableGroupLabel(labels, tgCfg.Name)

	tgCR := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tgNameHash,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName:   dbName,
			TableGroupName: tgCfg.Name,
			IsDefault:      tgCfg.Default,
			Images: multigresv1alpha1.ShardImages{
				MultiOrch:        cluster.Spec.Images.MultiOrch,
				MultiPooler:      cluster.Spec.Images.MultiPooler,
				Postgres:         cluster.Spec.Images.Postgres,
				ImagePullPolicy:  cluster.Spec.Images.ImagePullPolicy,
				ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
			},
			GlobalTopoServer: globalTopoRef,
			Shards:           resolvedShards,
		},
	}

	if err := controllerutil.SetControllerReference(cluster, tgCR, scheme); err != nil {
		return nil, err
	}

	return tgCR, nil
}
