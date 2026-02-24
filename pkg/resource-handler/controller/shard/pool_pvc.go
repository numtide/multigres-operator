package shard

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	nameutil "github.com/numtide/multigres-operator/pkg/util/name"
)

// BuildPoolDataPVCName constructs the PVC name for a specific pod index.
func BuildPoolDataPVCName(shard *multigresv1alpha1.Shard, poolName, cellName string, index int) string {
	clusterName := shard.Labels["multigres.com/cluster"]
	baseName := nameutil.JoinWithConstraints(
		nameutil.PodConstraints,
		clusterName,
		string(shard.Spec.DatabaseName),
		string(shard.Spec.TableGroupName),
		string(shard.Spec.ShardName),
		"pool",
		poolName,
		cellName,
	)
	return fmt.Sprintf("data-%s-%d", baseName, index)
}

// BuildPoolDataPVC creates a PersistentVolumeClaim for a pool pod's data volume.
func BuildPoolDataPVC(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	index int,
	scheme *runtime.Scheme,
) (*corev1.PersistentVolumeClaim, error) {
	pvcName := BuildPoolDataPVCName(shard, poolName, cellName, index)
	labels := buildPoolLabelsWithCell(shard, poolName, cellName, poolSpec)
	clusterName := shard.Labels["multigres.com/cluster"]
	metadata.AddClusterLabel(labels, clusterName)

	var storageClass *string
	storageSize := DefaultDataVolumeSize
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}

	if poolSpec.Storage.Class != "" {
		storageClass = &poolSpec.Storage.Class
	}
	if poolSpec.Storage.Size != "" {
		storageSize = poolSpec.Storage.Size
	}
	if len(poolSpec.Storage.AccessModes) > 0 {
		accessModes = poolSpec.Storage.AccessModes
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if storageClass != nil {
		pvc.Spec.StorageClassName = storageClass
	}

	if err := ctrl.SetControllerReference(shard, pvc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return pvc, nil
}
