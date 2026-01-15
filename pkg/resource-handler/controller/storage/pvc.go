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
//   - accessModes: List of access modes (e.g., [ReadWriteOnce]). Defaults to ReadWriteOnce if empty.
func BuildPVCTemplate(
	name string,
	storageClassName *string,
	storageSize string,
	accessModes []corev1.PersistentVolumeAccessMode,
) corev1.PersistentVolumeClaim {
	if len(accessModes) == 0 {
		accessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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

	if storageClassName != nil {
		pvc.Spec.StorageClassName = storageClassName
	}

	return pvc
}
