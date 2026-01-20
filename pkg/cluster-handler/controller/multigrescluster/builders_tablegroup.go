package multigrescluster

import (
	"crypto/sha1"
	"encoding/hex"
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
	// If the generated name is too long (> 40 chars), we hash the database and tablegroup names
	// to ensure the resulting Shard/Pod names (which append suffixes) stay within the 63-char label limit.
	// The StatefulSet controller adds a hash suffix (~10 chars) to its ControllerRevision labels,
	// so we target a safe limit of ~50 chars total for the StatefulSet name.
	// The StatefulSet controller adds a hash suffix (~10 chars) to its ControllerRevision/Pod labels.
	// With standard suffixes (-0-pool-default-zone-a = 22 chars), we have ~31 chars left for the base name.
	// We set the limit to 28 to be safe.
	if len(tgNameFull) > 28 {
		h := sha1.New()
		h.Write([]byte(dbName + "-" + tgCfg.Name))
		hash := hex.EncodeToString(h.Sum(nil))
		// Use cluster name + first 8 chars of hash
		tgNameFull = fmt.Sprintf("%s-%s", cluster.Name, hash[:8])
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
