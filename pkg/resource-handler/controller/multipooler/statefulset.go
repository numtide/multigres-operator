package multipooler

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/storage"
)

const (
	// ComponentName is the component label value for multipooler resources
	ComponentName = "multipooler"

	// DefaultReplicas is the default number of multipooler replicas
	DefaultReplicas int32 = 1

	// DefaultStorageSize is the default storage size for PostgreSQL data
	DefaultStorageSize = "10Gi"

	// DataVolumeName is the name of the data volume
	DataVolumeName = "pgdata"

	// DataMountPath is the mount path for PostgreSQL data
	DataMountPath = "/var/lib/postgresql/data"
)

// BuildStatefulSet creates a StatefulSet for the MultiPooler cluster.
// Returns a deterministic StatefulSet based on the MultiPooler spec.
func BuildStatefulSet(
	multipooler *multigresv1alpha1.MultiPooler,
	scheme *runtime.Scheme,
) (*appsv1.StatefulSet, error) {
	replicas := DefaultReplicas
	if multipooler.Spec.Replicas != nil {
		replicas = *multipooler.Spec.Replicas
	}

	headlessServiceName := multipooler.Name + "-headless"
	labels := metadata.BuildStandardLabels(multipooler.Name, ComponentName, multipooler.Spec.CellName)
	podLabels := metadata.MergeLabels(labels, multipooler.Spec.PodLabels)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      multipooler.Name,
			Namespace: multipooler.Namespace,
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
					Labels:      podLabels,
					Annotations: multipooler.Spec.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: multipooler.Spec.ServiceAccountName,
					ImagePullSecrets:   multipooler.Spec.ImagePullSecrets,
					// Init containers: pgctld copies binary, multipooler is a native sidecar
					InitContainers: []corev1.Container{
						buildPgctldInitContainer(multipooler),
						buildMultiPoolerSidecar(multipooler),
					},
					// Postgres is the main container (runs pgctld binary)
					Containers: []corev1.Container{
						buildPostgresContainer(multipooler),
					},
					// Shared volume for pgctld binary
					Volumes: []corev1.Volume{
						{
							Name: PgctldVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					Affinity:                  multipooler.Spec.Affinity,
					Tolerations:               multipooler.Spec.Tolerations,
					NodeSelector:              multipooler.Spec.NodeSelector,
					TopologySpreadConstraints: multipooler.Spec.TopologySpreadConstraints,
				},
			},
			VolumeClaimTemplates: buildVolumeClaimTemplates(multipooler),
		},
	}

	if err := ctrl.SetControllerReference(multipooler, sts, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return sts, nil
}

// buildVolumeClaimTemplates creates the PVC templates for PostgreSQL data storage.
// Caller decides whether to use VolumeClaimTemplate or build from simple fields.
func buildVolumeClaimTemplates(multipooler *multigresv1alpha1.MultiPooler) []corev1.PersistentVolumeClaim {
	if multipooler.Spec.VolumeClaimTemplate != nil {
		return []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: DataVolumeName,
				},
				Spec: *multipooler.Spec.VolumeClaimTemplate,
			},
		}
	}

	storageSize := DefaultStorageSize
	if multipooler.Spec.StorageSize != "" {
		storageSize = multipooler.Spec.StorageSize
	}

	return []corev1.PersistentVolumeClaim{
		storage.BuildPVCTemplate(DataVolumeName, multipooler.Spec.StorageClassName, storageSize),
	}
}
