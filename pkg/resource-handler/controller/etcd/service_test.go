package etcd

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildHeadlessService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	// NOTE: error path is tested as a part of the reconciliation loop.
	tests := map[string]struct {
		etcd   *multigresv1alpha1.Etcd
		scheme *runtime.Scheme
		want   *corev1.Service
	}{
		"minimal spec": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd-headless",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-etcd",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Etcd",
							Name:               "test-etcd",
							UID:                "test-uid",
							Controller:         boolPtr(true),
							BlockOwnerDeletion: boolPtr(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-etcd",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					Ports: []corev1.ServicePort{
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
					PublishNotReadyAddresses: true,
				},
			},
		},
		"with cellName": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "etcd-zone1",
					Namespace: "production",
					UID:       "zone1-uid",
				},
				Spec: multigresv1alpha1.EtcdSpec{
					CellName: "zone1",
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "etcd-zone1-headless",
					Namespace: "production",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "etcd-zone1",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Etcd",
							Name:               "etcd-zone1",
							UID:                "zone1-uid",
							Controller:         boolPtr(true),
							BlockOwnerDeletion: boolPtr(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "etcd-zone1",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
					},
					Ports: []corev1.ServicePort{
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
					PublishNotReadyAddresses: true,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildHeadlessService(tc.etcd, tc.scheme)
			if err != nil {
				t.Fatalf("BuildHeadlessService() unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildHeadlessService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildClientService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		etcd   *multigresv1alpha1.Etcd
		scheme *runtime.Scheme
		want   *corev1.Service
	}{
		"minimal spec": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.EtcdSpec{},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-etcd",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-etcd",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Etcd",
							Name:               "test-etcd",
							UID:                "test-uid",
							Controller:         boolPtr(true),
							BlockOwnerDeletion: boolPtr(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-etcd",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "client",
							Port:       2379,
							TargetPort: intstr.FromString("client"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
		},
		"with cellName": {
			etcd: &multigresv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "etcd-zone2",
					Namespace: "production",
					UID:       "zone2-uid",
				},
				Spec: multigresv1alpha1.EtcdSpec{
					CellName: "zone2",
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "etcd-zone2",
					Namespace: "production",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "etcd-zone2",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone2",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Etcd",
							Name:               "etcd-zone2",
							UID:                "zone2-uid",
							Controller:         boolPtr(true),
							BlockOwnerDeletion: boolPtr(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "etcd-zone2",
						"app.kubernetes.io/component":  "etcd",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone2",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "client",
							Port:       2379,
							TargetPort: intstr.FromString("client"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildClientService(tc.etcd, tc.scheme)
			if err != nil {
				t.Fatalf("BuildClientService() unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildClientService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
