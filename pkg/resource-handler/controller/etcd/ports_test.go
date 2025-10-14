package etcd

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildContainerPorts(t *testing.T) {
	tests := map[string]struct {
		etcd *multigresv1alpha1.Etcd
		want []corev1.ContainerPort
	}{
		"default ports": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildContainerPorts(tc.etcd)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildContainerPorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildHeadlessServicePorts(t *testing.T) {
	tests := map[string]struct {
		etcd *multigresv1alpha1.Etcd
		want []corev1.ServicePort
	}{
		"default ports": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			want: []corev1.ServicePort{
				{
					Name:       "client",
					Port:       2379,
					TargetPort: intstr.FromString("client"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "peer",
					Port:       2380,
					TargetPort: intstr.FromString("peer"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildHeadlessServicePorts(tc.etcd)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildHeadlessServicePorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildClientServicePorts(t *testing.T) {
	tests := map[string]struct {
		etcd *multigresv1alpha1.Etcd
		want []corev1.ServicePort
	}{
		"default port": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			want: []corev1.ServicePort{
				{
					Name:       "client",
					Port:       2379,
					TargetPort: intstr.FromString("client"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildClientServicePorts(tc.etcd)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildClientServicePorts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
