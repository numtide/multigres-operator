package shard

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
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

// BuildPoolStatefulSet creates a StatefulSet for a shard pool in a specific cell.
// For pools spanning multiple cells, this function should be called once per cell.
// The StatefulSet includes:
// - Init container: pgctld-init (copies pgctld binary to shared emptyDir)
// - Init container (native sidecar): multipooler (with restartPolicy: Always)
// - Main container: postgres (runs with pgctld binary)
// - EmptyDir volume for pgctld binary sharing
// - PVC for postgres data
func BuildPoolStatefulSet(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	scheme *runtime.Scheme,
) (*appsv1.StatefulSet, error) {
	name := buildPoolNameWithCell(shard.Name, poolName, cellName)
	headlessServiceName := name + "-headless"
	labels := buildPoolLabelsWithCell(shard, poolName, cellName, poolSpec)

	// Each StatefulSet in a cell has ReplicasPerCell replicas
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
					// FSGroup ensures PVC is writable by postgres user (required for initdb)
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: ptr.To(int64(999)), // postgres group in postgres:17 image
					},
					InitContainers: []corev1.Container{
						// ALTERNATIVE: Add init container to copy pgctld and pgbackrest binaries
						// to emptyDir, enabling use of stock postgres:17 image
						// buildPgctldInitContainer(shard),
						buildMultiPoolerSidecar(shard, poolSpec, poolName, cellName),
					},
					Containers: []corev1.Container{
						buildPgctldContainer(shard, poolSpec),
						// ALTERNATIVE: Use stock postgres:17 with copied binaries
						// buildPostgresContainer(shard, poolSpec),
					},
					Volumes: []corev1.Volume{
						// ALTERNATIVE: Add emptyDir volume for binary copy
						// buildPgctldVolume(),
						buildBackupVolume(name),
						buildSocketDirVolume(),
						buildPgHbaVolume(),
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

// BuildBackupPVC creates a standalone PVC for backup storage shared across all pods in a pool.
// This PVC is created independently of the StatefulSet and referenced by all pods.
// For single-node clusters (kind, minikube), uses ReadWriteOnce (all pods on same node).
// For multi-node production, configure BackupStorage.Class to a storage class supporting ReadWriteMany.
func BuildBackupPVC(
	shard *multigresv1alpha1.Shard,
	poolName string,
	cellName string,
	poolSpec multigresv1alpha1.PoolSpec,
	scheme *runtime.Scheme,
) (*corev1.PersistentVolumeClaim, error) {
	name := buildPoolNameWithCell(shard.Name, poolName, cellName)
	pvcName := "backup-data-" + name
	labels := buildPoolLabelsWithCell(shard, poolName, cellName, poolSpec)

	// Use BackupStorage if specified, otherwise inherit from Storage
	var storageClass *string
	storageSize := "10Gi" // Default backup storage size

	if poolSpec.BackupStorage.Class != "" {
		storageClass = &poolSpec.BackupStorage.Class
	} else if poolSpec.Storage.Class != "" {
		storageClass = &poolSpec.Storage.Class
	}

	if poolSpec.BackupStorage.Size != "" {
		storageSize = poolSpec.BackupStorage.Size
	}

	// Default to ReadWriteOnce for single-node clusters.
	// TODO: When StorageSpec.AccessMode is added, use:
	//   accessMode := corev1.ReadWriteOnce
	//   if poolSpec.BackupStorage.AccessMode != "" {
	//       accessMode = poolSpec.BackupStorage.AccessMode
	//   }
	accessMode := corev1.ReadWriteOnce

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				accessMode,
			},
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
