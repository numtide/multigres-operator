package multipooler

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
		multipooler *multigresv1alpha1.MultiPooler
		scheme      *runtime.Scheme
		want        *corev1.Service
	}{
		"minimal spec": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler-headless",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multipooler",
						"app.kubernetes.io/component":  "multipooler",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "MultiPooler",
							Name:               "test-multipooler",
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
						"app.kubernetes.io/instance":   "test-multipooler",
						"app.kubernetes.io/component":  "multipooler",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       15200,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       15270,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       5432,
							TargetPort: intstr.FromString("postgres"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
					PublishNotReadyAddresses: true,
				},
			},
		},
		"with cellName": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multipooler-cell1",
					Namespace: "production",
					UID:       "cell1-uid",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					CellName: "cell-1",
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multipooler-cell1-headless",
					Namespace: "production",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "multipooler-cell1",
						"app.kubernetes.io/component":  "multipooler",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "cell-1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "MultiPooler",
							Name:               "multipooler-cell1",
							UID:                "cell1-uid",
							Controller:         boolPtr(true),
							BlockOwnerDeletion: boolPtr(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "multipooler-cell1",
						"app.kubernetes.io/component":  "multipooler",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "cell-1",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       15200,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       15270,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       5432,
							TargetPort: intstr.FromString("postgres"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
					PublishNotReadyAddresses: true,
				},
			},
		},
		"with custom ports": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					HTTPPort:     8080,
					GRPCPort:     9090,
					PostgresPort: 5433,
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler-headless",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multipooler",
						"app.kubernetes.io/component":  "multipooler",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "MultiPooler",
							Name:               "test-multipooler",
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
						"app.kubernetes.io/instance":   "test-multipooler",
						"app.kubernetes.io/component":  "multipooler",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       8080,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       9090,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       5433,
							TargetPort: intstr.FromString("postgres"),
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
			got, err := BuildHeadlessService(tc.multipooler, tc.scheme)
			if err != nil {
				t.Fatalf("BuildHeadlessService() unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildHeadlessService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
