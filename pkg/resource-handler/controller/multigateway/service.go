package multigateway

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// BuildService creates a client Service for external access to Etcd.
// This service load balances across all etcd members.
func BuildService(
	mg *multigresv1alpha1.MultiGateway,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	labels := metadata.BuildStandardLabels(mg.Name, ComponentName, mg.Spec.CellName)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mg.Name,
			Namespace: mg.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports:    buildServicePorts(mg),
		},
	}

	if err := ctrl.SetControllerReference(mg, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}
