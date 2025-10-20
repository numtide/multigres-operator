package multiorch

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	// NOTE: error path is tested as a part of the reconciliation loop.
	tests := map[string]struct {
		multiorch *multigresv1alpha1.MultiOrch
		scheme    *runtime.Scheme
		want      *corev1.Service
	}{
		"minimal spec": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "MultiOrch",
							Name:               "test-multiorch",
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
						"app.kubernetes.io/instance":   "test-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					Ports: buildServicePorts(&multigresv1alpha1.MultiOrch{}),
				},
			},
		},
		"custom service type and annotations": {
			multiorch: &multigresv1alpha1.MultiOrch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiOrchSpec{
					ServiceType: corev1.ServiceTypeLoadBalancer,
					ServiceAnnotations: map[string]string{
						"cloud.google.com/load-balancer-type": "Internal",
					},
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					Annotations: map[string]string{
						"cloud.google.com/load-balancer-type": "Internal",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "MultiOrch",
							Name:               "test-multiorch",
							UID:                "test-uid",
							Controller:         boolPtr(true),
							BlockOwnerDeletion: boolPtr(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
					},
					Ports: buildServicePorts(&multigresv1alpha1.MultiOrch{}),
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildService(tc.multiorch, tc.scheme)
			if err != nil {
				t.Fatalf("BuildService() unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
