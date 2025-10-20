package multiorch

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// BuildService creates a Service for external access to MultiOrch.
func BuildService(
	multiorch *multigresv1alpha1.MultiOrch,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	labels := metadata.BuildStandardLabels(multiorch.Name, ComponentName, multiorch.Spec.CellName)

	serviceType := corev1.ServiceTypeClusterIP
	if multiorch.Spec.ServiceType != "" {
		serviceType = multiorch.Spec.ServiceType
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        multiorch.Name,
			Namespace:   multiorch.Namespace,
			Labels:      labels,
			Annotations: multiorch.Spec.ServiceAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: labels,
			Ports:    buildServicePorts(multiorch),
		},
	}

	if err := ctrl.SetControllerReference(multiorch, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}
