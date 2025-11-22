package multigateway

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

	tests := map[string]struct {
		mg      *multigresv1alpha1.MultiGateway
		scheme  *runtime.Scheme
		want    *corev1.Service
		wantErr bool
	}{
		"minimal spec": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "MultiGateway",
							Name:               "test-multigateway",
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
						"app.kubernetes.io/instance":   "test-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: buildServicePorts(&multigresv1alpha1.MultiGateway{}),
				},
			},
		},
		"scheme with incorrect type - should error": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			scheme:  runtime.NewScheme(), // empty scheme with incorrect type
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildService(tc.mg, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildClientService() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildClientService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
