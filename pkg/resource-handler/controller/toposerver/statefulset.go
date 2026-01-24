package toposerver

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
	// ComponentName is the component label value for toposerver resources
	ComponentName = "toposerver"

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

// BuildStatefulSet creates a StatefulSet for the TopoServer cluster.
// Returns a deterministic StatefulSet based on the TopoServer spec.
func BuildStatefulSet(
	toposerver *multigresv1alpha1.TopoServer,
	scheme *runtime.Scheme,
) (*appsv1.StatefulSet, error) {
	replicas := DefaultReplicas
	if toposerver.Spec.Etcd != nil && toposerver.Spec.Etcd.Replicas != nil {
		replicas = *toposerver.Spec.Etcd.Replicas
	}

	image := DefaultImage
	if toposerver.Spec.Etcd != nil && toposerver.Spec.Etcd.Image != "" {
		image = toposerver.Spec.Etcd.Image
	}

	headlessServiceName := toposerver.Name + "-headless"
	// TODO: Support cell-local TopoServers by adding CellName field to TopoServerSpec
	// For now, TopoServer is always global topology
	clusterName := toposerver.Labels["multigres.com/cluster"]
	labels := metadata.BuildStandardLabels(clusterName, ComponentName)
	labels = metadata.MergeLabels(labels, toposerver.GetObjectMeta().GetLabels())

	resources := corev1.ResourceRequirements{}
	if toposerver.Spec.Etcd != nil {
		resources = toposerver.Spec.Etcd.Resources
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      toposerver.Name,
			Namespace: toposerver.Namespace,
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
					Containers: []corev1.Container{
						{
							Name:      "etcd",
							Image:     image,
							Resources: resources,
							Env: buildContainerEnv(
								toposerver.Name,
								toposerver.Namespace,
								replicas,
								headlessServiceName,
							),
							Ports: buildContainerPorts(toposerver),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      DataVolumeName,
									MountPath: DataMountPath,
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: buildVolumeClaimTemplates(toposerver),
		},
	}

	if err := ctrl.SetControllerReference(toposerver, sts, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return sts, nil
}

// buildVolumeClaimTemplates creates the PVC templates for etcd data storage.
func buildVolumeClaimTemplates(
	toposerver *multigresv1alpha1.TopoServer,
) []corev1.PersistentVolumeClaim {
	var storageClass *string
	storageSize := DefaultStorageSize
	var accessModes []corev1.PersistentVolumeAccessMode

	if toposerver.Spec.Etcd != nil {
		if toposerver.Spec.Etcd.Storage.Class != "" {
			storageClass = &toposerver.Spec.Etcd.Storage.Class
		}
		if toposerver.Spec.Etcd.Storage.Size != "" {
			storageSize = toposerver.Spec.Etcd.Storage.Size
		}
		accessModes = toposerver.Spec.Etcd.Storage.AccessModes
	}

	return []corev1.PersistentVolumeClaim{
		storage.BuildPVCTemplate(DataVolumeName, storageClass, storageSize, accessModes),
	}
}
