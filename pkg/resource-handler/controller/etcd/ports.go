package etcd

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// ClientPort is the default port for etcd client connections.
	ClientPort = 2379

	// PeerPort is the default port for etcd peer connections.
	PeerPort = 2380
)

// PortOption configures port settings for etcd containers.
type PortOption func(*portOptions)

type portOptions struct {
	clientPort int32
	peerPort   int32
}

// WithClientPort overrides the default client port.
func WithClientPort(port int32) PortOption {
	return func(o *portOptions) {
		o.clientPort = port
	}
}

// WithPeerPort overrides the default peer port.
func WithPeerPort(port int32) PortOption {
	return func(o *portOptions) {
		o.peerPort = port
	}
}

// buildContainerPorts creates the port definitions for the etcd container.
// These ports are used in both the StatefulSet container spec and Service
// definitions.
func buildContainerPorts(opts ...PortOption) []corev1.ContainerPort {
	options := &portOptions{
		clientPort: ClientPort,
		peerPort:   PeerPort,
	}

	for _, opt := range opts {
		opt(options)
	}

	return []corev1.ContainerPort{
		{
			Name:          "client",
			ContainerPort: options.clientPort,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "peer",
			ContainerPort: options.peerPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}
}

// buildHeadlessServicePorts creates service ports for the headless service.
// Includes both client and peer ports for StatefulSet pod discovery.
func buildHeadlessServicePorts(opts ...PortOption) []corev1.ServicePort {
	options := &portOptions{
		clientPort: ClientPort,
		peerPort:   PeerPort,
	}

	for _, opt := range opts {
		opt(options)
	}

	return []corev1.ServicePort{
		{
			Name:       "client",
			Port:       options.clientPort,
			TargetPort: intstr.FromString("client"),
			Protocol:   corev1.ProtocolTCP,
		},
		{
			Name:       "peer",
			Port:       options.peerPort,
			TargetPort: intstr.FromString("peer"),
			Protocol:   corev1.ProtocolTCP,
		},
	}
}

// buildClientServicePorts creates service ports for the client service.
// Only includes the client port for external access.
func buildClientServicePorts(opts ...PortOption) []corev1.ServicePort {
	options := &portOptions{
		clientPort: ClientPort,
		peerPort:   PeerPort,
	}

	for _, opt := range opts {
		opt(options)
	}

	return []corev1.ServicePort{
		{
			Name:       "client",
			Port:       options.clientPort,
			TargetPort: intstr.FromString("client"),
			Protocol:   corev1.ProtocolTCP,
		},
	}
}
