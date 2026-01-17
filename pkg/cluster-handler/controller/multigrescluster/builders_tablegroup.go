package multigrescluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// BuildTableGroup constructs the desired TableGroup resource.
func BuildTableGroup(
	cluster *multigresv1alpha1.MultigresCluster,
	dbName string,
	tgCfg *multigresv1alpha1.TableGroupConfig,
	resolvedShards []multigresv1alpha1.ShardResolvedSpec,
	globalTopoRef multigresv1alpha1.GlobalTopoServerRef,
	scheme *runtime.Scheme,
) (*multigresv1alpha1.TableGroup, error) {
	tgNameFull := fmt.Sprintf("%s-%s-%s", cluster.Name, dbName, tgCfg.Name)
	if len(tgNameFull) > 50 {
		return nil, fmt.Errorf(
			"TableGroup name '%s' exceeds 50 characters; limit required to allow for shard resource suffixing",
			tgNameFull,
		)
	}

	tgCR := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tgNameFull,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"multigres.com/cluster":    cluster.Name,
				"multigres.com/database":   dbName,
				"multigres.com/tablegroup": tgCfg.Name,
			},
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
