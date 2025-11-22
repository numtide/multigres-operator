package shard

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildMultiOrchDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		shard   *multigresv1alpha1.Shard
		scheme  *runtime.Scheme
		want    *appsv1.Deployment
		wantErr bool
	}{
		"minimal spec - all defaults": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"us-west-1a"}, // 1 cell = 1 replica
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-shard-multiorch",
						"app.kubernetes.io/component":  MultiOrchComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "test-shard",
							UID:                "test-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)), // Replicas = len(cells)
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-shard-multiorch",
							"app.kubernetes.io/component":  MultiOrchComponentName,
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-shard-multiorch",
								"app.kubernetes.io/component":  MultiOrchComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "multiorch",
									Image: DefaultMultiOrchImage,
									Ports: buildMultiOrchContainerPorts(),
								},
							},
						},
					},
				},
			},
		},
		"with different shard name": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-shard",
					Namespace: "prod-ns",
					UID:       "prod-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []string{"zone1", "zone2"}, // 2 cells = 2 replicas
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-shard-multiorch",
					Namespace: "prod-ns",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "production-shard-multiorch",
						"app.kubernetes.io/component":  MultiOrchComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "production-shard",
							UID:                "prod-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(2)), // Replicas = len(cells) = 2
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "production-shard-multiorch",
							"app.kubernetes.io/component":  MultiOrchComponentName,
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "production-shard-multiorch",
								"app.kubernetes.io/component":  MultiOrchComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "multiorch",
									Image: DefaultMultiOrchImage,
									Ports: buildMultiOrchContainerPorts(),
								},
							},
						},
					},
				},
			},
		},
		"invalid scheme - should error": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			scheme:  runtime.NewScheme(), // empty scheme
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildMultiOrchDeployment(tc.shard, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildMultiOrchDeployment() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildMultiOrchDeployment() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildMultiOrchService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		shard   *multigresv1alpha1.Shard
		scheme  *runtime.Scheme
		want    *corev1.Service
		wantErr bool
	}{
		"minimal spec": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-shard-multiorch",
						"app.kubernetes.io/component":  MultiOrchComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "test-shard",
							UID:                "test-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-shard-multiorch",
						"app.kubernetes.io/component":  MultiOrchComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       DefaultMultiOrchHTTPPort,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       DefaultMultiOrchGRPCPort,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
		},
		"with different namespace": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-shard",
					Namespace: "prod-ns",
					UID:       "prod-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-shard-multiorch",
					Namespace: "prod-ns",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "production-shard-multiorch",
						"app.kubernetes.io/component":  MultiOrchComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "production-shard",
							UID:                "prod-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Selector: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "production-shard-multiorch",
						"app.kubernetes.io/component":  MultiOrchComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       DefaultMultiOrchHTTPPort,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       DefaultMultiOrchGRPCPort,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			},
		},
		"invalid scheme - should error": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			scheme:  runtime.NewScheme(), // empty scheme
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildMultiOrchService(tc.shard, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildMultiOrchService() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildMultiOrchService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
