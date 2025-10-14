package etcd

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
	// ComponentName is the component label value for etcd resources
	ComponentName = "etcd"

	// DefaultReplicas is the default number of etcd replicas
	DefaultReplicas int32 = 3

	// DefaultImage is the default etcd container image
	DefaultImage = "gcr.io/etcd-development/etcd:v3.5.9"

	// DefaultStorageSize is the default storage size for etcd data
	DefaultStorageSize = "10Gi"

	// DataVolumeName is the name of the data volume
	DataVolumeName = "data"

	// DataMountPath is the mount path for etcd data
	DataMountPath = "/var/lib/etcd"
)

// BuildStatefulSet creates a StatefulSet for the Etcd cluster.
// Returns a deterministic StatefulSet based on the Etcd spec.
func BuildStatefulSet(
	etcd *multigresv1alpha1.Etcd,
	scheme *runtime.Scheme,
) (*appsv1.StatefulSet, error) {
	replicas := DefaultReplicas
	// TODO: Debatable whether this defaulting makes sense.
	if etcd.Spec.Replicas != nil {
		replicas = *etcd.Spec.Replicas
	}

	image := DefaultImage
	if etcd.Spec.Image != "" {
		image = etcd.Spec.Image
	}

	headlessServiceName := etcd.Name + "-headless"
	labels := metadata.BuildStandardLabels(etcd.Name, ComponentName, etcd.Spec.CellName)
	podLabels := metadata.MergeLabels(labels, etcd.Spec.PodLabels)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      etcd.Name,
			Namespace: etcd.Namespace,
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
					Annotations: etcd.Spec.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: etcd.Spec.ServiceAccountName,
					ImagePullSecrets:   etcd.Spec.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:      "etcd",
							Image:     image,
							Resources: etcd.Spec.Resources,
							Env: buildEtcdEnv(
								etcd.Name,
								etcd.Namespace,
								replicas,
								headlessServiceName,
							),
							Ports: buildContainerPorts(etcd),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      DataVolumeName,
									MountPath: DataMountPath,
								},
							},
						},
					},
					Affinity:                  etcd.Spec.Affinity,
					Tolerations:               etcd.Spec.Tolerations,
					NodeSelector:              etcd.Spec.NodeSelector,
					TopologySpreadConstraints: etcd.Spec.TopologySpreadConstraints,
				},
			},
			VolumeClaimTemplates: buildVolumeClaimTemplates(etcd),
		},
	}

	if err := ctrl.SetControllerReference(etcd, sts, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return sts, nil
}

// buildVolumeClaimTemplates creates the PVC templates for etcd data storage.
// Caller decides whether to use VolumeClaimTemplate or build from simple fields.
func buildVolumeClaimTemplates(etcd *multigresv1alpha1.Etcd) []corev1.PersistentVolumeClaim {
	if etcd.Spec.VolumeClaimTemplate != nil {
		return []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: DataVolumeName,
				},
				Spec: *etcd.Spec.VolumeClaimTemplate,
			},
		}
	}

	storageSize := DefaultStorageSize
	if etcd.Spec.StorageSize != "" {
		storageSize = etcd.Spec.StorageSize
	}

	return []corev1.PersistentVolumeClaim{
		storage.BuildPVCTemplate(DataVolumeName, etcd.Spec.StorageClassName, storageSize),
	}
}
