package multigateway

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func int32Ptr(i int32) *int32 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func TestBuildDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		mg      *multigresv1alpha1.MultiGateway
		scheme  *runtime.Scheme
		want    *appsv1.Deployment
		wantErr bool
	}{
		"minimal spec - all defaults": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
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
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-multigateway",
							"app.kubernetes.io/component":  "multigateway",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "multigres-global-topo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-multigateway",
								"app.kubernetes.io/component":  "multigateway",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "multigres-global-topo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multigateway",
									Image:     DefaultImage,
									Resources: corev1.ResourceRequirements{},
									Env:       buildContainerEnv(),
									Ports: buildContainerPorts(
										&multigresv1alpha1.MultiGateway{},
									),
								},
							},
						},
					},
				},
			},
		},
		"custom replicas and image": {
			mg: &multigresv1alpha1.MultiGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiGatewaySpec{
					Replicas: int32Ptr(3),
					Image:    "foo/bar:1.2.3",
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "multigres-global-topo",
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
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-multigateway",
							"app.kubernetes.io/component":  "multigateway",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "multigres-global-topo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-multigateway",
								"app.kubernetes.io/component":  "multigateway",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "multigres-global-topo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multigateway",
									Image:     "foo/bar:1.2.3",
									Resources: corev1.ResourceRequirements{},
									Env:       buildContainerEnv(),
									Ports: buildContainerPorts(
										&multigresv1alpha1.MultiGateway{},
									),
								},
							},
						},
					},
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
			got, err := BuildDeployment(tc.mg, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildDeployment() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildDeployment() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
