package etcd

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// BuildHeadlessService creates a headless Service for the Etcd StatefulSet.
// Headless services are required for StatefulSet pod DNS records.
func BuildHeadlessService(
	etcd *multigresv1alpha1.Etcd,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	labels := metadata.BuildStandardLabels(etcd.Name, ComponentName)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      etcd.Name + "-headless",
			Namespace: etcd.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 labels,
			Ports:                    buildHeadlessServicePorts(etcd),
			PublishNotReadyAddresses: true,
		},
	}

	if err := ctrl.SetControllerReference(etcd, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}

// BuildClientService creates a client Service for external access to Etcd.
// This service load balances across all etcd members.
func BuildClientService(
	etcd *multigresv1alpha1.Etcd,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	labels := metadata.BuildStandardLabels(etcd.Name, ComponentName)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      etcd.Name,
			Namespace: etcd.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports:    buildClientServicePorts(etcd),
		},
	}

	if err := ctrl.SetControllerReference(etcd, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}
