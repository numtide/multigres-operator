package toposerver

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	// ClientPort is the default port for etcd client connections.
	ClientPort int32 = 2379

	// PeerPort is the default port for etcd peer connections.
	PeerPort int32 = 2380
)

// buildContainerPorts creates the port definitions for the etcd container.
// Uses default ports since TopoServerSpec doesn't have port configuration yet.
func buildContainerPorts(toposerver *multigresv1alpha1.TopoServer) []corev1.ContainerPort {
	clientPort := ClientPort
	peerPort := PeerPort

	// TODO: When TopoServerSpec has port fields, use them:
	// if toposerver.Spec.ClientPort != 0 {
	//     clientPort = toposerver.Spec.ClientPort
	// }
	// if toposerver.Spec.PeerPort != 0 {
	//     peerPort = toposerver.Spec.PeerPort
	// }

	return []corev1.ContainerPort{
		{
			Name:          "client",
			ContainerPort: clientPort,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "peer",
			ContainerPort: peerPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}
}

// buildHeadlessServicePorts creates service ports for the headless service.
// Includes both client and peer ports for StatefulSet pod discovery.
func buildHeadlessServicePorts(toposerver *multigresv1alpha1.TopoServer) []corev1.ServicePort {
	clientPort := ClientPort
	peerPort := PeerPort

	// TODO: When TopoServerSpec has port fields, use them:
	// if toposerver.Spec.ClientPort != 0 {
	//     clientPort = toposerver.Spec.ClientPort
	// }
	// if toposerver.Spec.PeerPort != 0 {
	//     peerPort = toposerver.Spec.PeerPort
	// }

	return []corev1.ServicePort{
		{
			Name:       "client",
			Port:       clientPort,
			TargetPort: intstr.FromString("client"),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "peer",
			Port:       peerPort,
			TargetPort: intstr.FromString("peer"),
			Protocol:   corev1.ProtocolTCP,
		},
	}
}

// buildClientServicePorts creates service ports for the client service.
// Only includes the client port for external access.
func buildClientServicePorts(toposerver *multigresv1alpha1.TopoServer) []corev1.ServicePort {
	clientPort := ClientPort

	// TODO: When TopoServerSpec has clientPort field, use it:
	// if toposerver.Spec.ClientPort != 0 {
	//     clientPort = toposerver.Spec.ClientPort
	// }

	return []corev1.ServicePort{
		{
			Name:       "client",
			Port:       clientPort,
			TargetPort: intstr.FromString("client"),
			Protocol:   corev1.ProtocolTCP,
		},
	}
}
