package cell

import (
	corev1 "k8s.io/api/core/v1"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
)

func tolerationsFromPlacement(placement *multigresv1alpha1.PodPlacementSpec) []corev1.Toleration {
	if placement == nil || len(placement.Tolerations) == 0 {
		return nil
	}
	return append([]corev1.Toleration(nil), placement.Tolerations...)
}
