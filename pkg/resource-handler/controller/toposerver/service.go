package toposerver

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

// BuildHeadlessService creates a headless Service for the TopoServer StatefulSet.
// Headless services are required for StatefulSet pod DNS records.
func BuildHeadlessService(
	toposerver *multigresv1alpha1.TopoServer,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	// TODO: Support cell-local TopoServers by adding CellName field to TopoServerSpec
	// For now, TopoServer is always global topology
	labels := metadata.BuildStandardLabels(toposerver.Name, ComponentName, "multigres-global-topo")

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      toposerver.Name + "-headless",
			Namespace: toposerver.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 labels,
			Ports:                    buildHeadlessServicePorts(toposerver),
			PublishNotReadyAddresses: true,
		},
	}

	if err := ctrl.SetControllerReference(toposerver, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}

// BuildClientService creates a client Service for external access to TopoServer.
// This service load balances across all etcd members.
func BuildClientService(
	toposerver *multigresv1alpha1.TopoServer,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	// TODO: Support cell-local TopoServers by adding CellName field to TopoServerSpec
	// For now, TopoServer is always global topology
	labels := metadata.BuildStandardLabels(toposerver.Name, ComponentName, "multigres-global-topo")

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      toposerver.Name,
			Namespace: toposerver.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports:    buildClientServicePorts(toposerver),
		},
	}

	if err := ctrl.SetControllerReference(toposerver, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}
