package shard

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// DefaultPoolReplicas is the default number of replicas for a pool
	DefaultPoolReplicas int32 = 1
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
	pool multigresv1alpha1.ShardPoolSpec,
	poolIndex int,
	scheme *runtime.Scheme,
) (*appsv1.StatefulSet, error) {
	poolName := buildPoolName(shard.Name, pool, poolIndex)
	headlessServiceName := poolName + "-headless"
	labels := buildPoolLabels(shard, pool, poolName)

	replicas := DefaultPoolReplicas
	if pool.Replicas != nil {
		replicas = *pool.Replicas
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolName,
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
						buildMultiPoolerSidecar(shard, pool),
					},
					// Postgres is the main container (runs pgctld binary)
					Containers: []corev1.Container{
						buildPostgresContainer(shard, pool),
					},
					// Shared volume for pgctld binary
					Volumes: []corev1.Volume{
						buildPgctldVolume(),
					},
					Affinity: pool.Affinity,
				},
			},
			VolumeClaimTemplates: buildPoolVolumeClaimTemplates(pool),
		},
	}

	if err := ctrl.SetControllerReference(shard, sts, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return sts, nil
}

// buildPoolVolumeClaimTemplates creates the PVC templates for a pool.
// Uses the pool's DataVolumeClaimTemplate if provided.
func buildPoolVolumeClaimTemplates(pool multigresv1alpha1.ShardPoolSpec) []corev1.PersistentVolumeClaim {
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: DataVolumeName,
		},
		Spec: pool.DataVolumeClaimTemplate,
	}

	// Set default VolumeMode if not specified
	if pvc.Spec.VolumeMode == nil {
		pvc.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
	}

	return []corev1.PersistentVolumeClaim{pvc}
}
