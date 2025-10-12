package etcd

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildContainerPorts(t *testing.T) {
	tests := map[string]struct {
		opts []PortOption
		want []corev1.ContainerPort
	}{
		"default ports": {
			opts: nil,
			want: []corev1.ContainerPort{
				{
					Name:          "client",
					ContainerPort: 2379,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "peer",
					ContainerPort: 2380,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		"custom client port": {
			opts: []PortOption{WithClientPort(3379)},
			want: []corev1.ContainerPort{
				{
					Name:          "client",
					ContainerPort: 3379,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "peer",
					ContainerPort: 2380,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		"custom peer port": {
			opts: []PortOption{WithPeerPort(3380)},
			want: []corev1.ContainerPort{
				{
					Name:          "client",
					ContainerPort: 2379,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "peer",
					ContainerPort: 3380,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		"both ports customized": {
			opts: []PortOption{
				WithClientPort(9379),
				WithPeerPort(9380),
			},
			want: []corev1.ContainerPort{
				{
					Name:          "client",
					ContainerPort: 9379,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "peer",
					ContainerPort: 9380,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		"zero port values - should use zero": {
			opts: []PortOption{
				WithClientPort(0),
				WithPeerPort(0),
			},
			want: []corev1.ContainerPort{
				{
					Name:          "client",
					ContainerPort: 0,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "peer",
					ContainerPort: 0,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildContainerPorts(tc.opts...)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildContainerPorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
