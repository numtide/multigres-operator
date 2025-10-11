// Package storage provides utilities for building PersistentVolumeClaim templates
// for StatefulSet-based components.
package storage

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildPVCTemplate creates a PersistentVolumeClaim for StatefulSet's volumeClaimTemplates.
// Builds from simple storage class name and size parameters.
//
// Parameters:
//   - name: Name for the volume claim (e.g., "data")
//   - storageClassName: Optional storage class name (nil uses cluster default)
//   - storageSize: Size of the volume (e.g., "10Gi")
func BuildPVCTemplate(
	name string,
	storageClassName *string,
	storageSize string,
) corev1.PersistentVolumeClaim {
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if storageClassName != nil {
		pvc.Spec.StorageClassName = storageClassName
	}

	return pvc
}
