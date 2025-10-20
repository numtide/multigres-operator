package multipooler

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// BuildHeadlessService creates a headless Service for the MultiPooler StatefulSet.
// Headless services are required for StatefulSet pod DNS records.
func BuildHeadlessService(
	multipooler *multigresv1alpha1.MultiPooler,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	labels := metadata.BuildStandardLabels(
		multipooler.Name,
		ComponentName,
		multipooler.Spec.CellName,
	)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      multipooler.Name + "-headless",
			Namespace: multipooler.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 labels,
			Ports:                    buildHeadlessServicePorts(multipooler),
			PublishNotReadyAddresses: true,
		},
	}

	if err := ctrl.SetControllerReference(multipooler, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}
