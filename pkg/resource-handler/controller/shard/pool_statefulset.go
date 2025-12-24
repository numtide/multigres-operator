package shard

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/storage"
)

const (
	// DefaultPoolReplicas is the default number of replicas for a pool
	DefaultPoolReplicas int32 = 1

	// DefaultDataVolumeSize is the default size for data volumes
	DefaultDataVolumeSize = "10Gi"
)

// BuildPoolStatefulSet creates a StatefulSet for a shard pool.
// The StatefulSet includes:
// - Init container: pgctld-init (copies pgctld binary to shared emptyDir)
// - Init container (native sidecar): multipooler (with restartPolicy: Always)
// - Main container: postgres (runs with pgctld binary)
// - EmptyDir volume for pgctld binary sharing
// - PVC for postgres data
func BuildPoolStatefulSet(
	shard *multigresv1alpha1.Shard,
	poolName string,
	poolSpec multigresv1alpha1.PoolSpec,
	scheme *runtime.Scheme,
) (*appsv1.StatefulSet, error) {
	name := buildPoolName(shard.Name, poolName)
	headlessServiceName := name + "-headless"
	labels := buildPoolLabels(shard, poolName, poolSpec)

	replicas := DefaultPoolReplicas
	if poolSpec.ReplicasPerCell != nil {
		replicas = *poolSpec.ReplicasPerCell
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: headlessServiceName,
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					// Init containers: pgctld copies binary, multipooler is a native sidecar
					InitContainers: []corev1.Container{
						buildPgctldInitContainer(shard),
						buildMultiPoolerSidecar(shard, poolSpec, poolName),
					},
					// Postgres is the main container (runs pgctld binary)
					Containers: []corev1.Container{
						buildPostgresContainer(shard, poolSpec),
					},
					// Shared volume for pgctld binary
					Volumes: []corev1.Volume{
						buildPgctldVolume(),
					},
					Affinity: poolSpec.Affinity,
				},
			},
			VolumeClaimTemplates: buildPoolVolumeClaimTemplates(poolSpec),
		},
	}

	if err := ctrl.SetControllerReference(shard, sts, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return sts, nil
}

// buildPoolVolumeClaimTemplates creates the PVC templates for a pool.
// Uses the pool's Storage configuration.
func buildPoolVolumeClaimTemplates(
	pool multigresv1alpha1.PoolSpec,
) []corev1.PersistentVolumeClaim {
	var storageClass *string
	storageSize := DefaultDataVolumeSize

	if pool.Storage.Class != "" {
		storageClass = &pool.Storage.Class
	}
	if pool.Storage.Size != "" {
		storageSize = pool.Storage.Size
	}

	return []corev1.PersistentVolumeClaim{
		storage.BuildPVCTemplate(DataVolumeName, storageClass, storageSize),
	}
}
