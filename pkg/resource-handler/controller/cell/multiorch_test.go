package cell

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
		cell    *multigresv1alpha1.Cell
		scheme  *runtime.Scheme
		want    *appsv1.Deployment
		wantErr bool
	}{
		"minimal spec - all defaults": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cell-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Cell",
							Name:               "test-cell",
							UID:                "test-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(DefaultMultiOrchReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-cell-multiorch",
							"app.kubernetes.io/component":  "multiorch",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "zone1",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-cell-multiorch",
								"app.kubernetes.io/component":  "multiorch",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone1",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multiorch",
									Image:     DefaultMultiOrchImage,
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{Name: "http", ContainerPort: MultiOrchHTTPPort, Protocol: corev1.ProtocolTCP},
										{Name: "grpc", ContainerPort: MultiOrchGRPCPort, Protocol: corev1.ProtocolTCP},
									},
								},
							},
						},
					},
				},
			},
		},
		"custom replicas": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-custom-replicas",
					Namespace: "test-ns",
					UID:       "custom-uid",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone2",
					MultiOrch: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(3)),
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-custom-replicas-multiorch",
					Namespace: "test-ns",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-custom-replicas-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone2",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Cell",
							Name:               "cell-custom-replicas",
							UID:                "custom-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-custom-replicas-multiorch",
							"app.kubernetes.io/component":  "multiorch",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "zone2",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-custom-replicas-multiorch",
								"app.kubernetes.io/component":  "multiorch",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone2",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multiorch",
									Image:     DefaultMultiOrchImage,
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{Name: "http", ContainerPort: MultiOrchHTTPPort, Protocol: corev1.ProtocolTCP},
										{Name: "grpc", ContainerPort: MultiOrchGRPCPort, Protocol: corev1.ProtocolTCP},
									},
								},
							},
						},
					},
				},
			},
		},
		"custom image": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-custom-image",
					Namespace: "default",
					UID:       "image-uid",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone3",
					Images: multigresv1alpha1.CellImagesSpec{
						MultiOrch: "custom/multiorch:v2.0.0",
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-custom-image-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-custom-image-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone3",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Cell",
							Name:               "cell-custom-image",
							UID:                "image-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(DefaultMultiOrchReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-custom-image-multiorch",
							"app.kubernetes.io/component":  "multiorch",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "zone3",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-custom-image-multiorch",
								"app.kubernetes.io/component":  "multiorch",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone3",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multiorch",
									Image:     "custom/multiorch:v2.0.0",
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{Name: "http", ContainerPort: MultiOrchHTTPPort, Protocol: corev1.ProtocolTCP},
										{Name: "grpc", ContainerPort: MultiOrchGRPCPort, Protocol: corev1.ProtocolTCP},
									},
								},
							},
						},
					},
				},
			},
		},
		"with affinity": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-affinity",
					Namespace: "default",
					UID:       "affinity-uid",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone4",
					MultiOrch: multigresv1alpha1.StatelessSpec{
						Affinity: &corev1.Affinity{
							PodAntiAffinity: &corev1.PodAntiAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
									{
										Weight: 100,
										PodAffinityTerm: corev1.PodAffinityTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"app.kubernetes.io/component": "multiorch",
												},
											},
											TopologyKey: "kubernetes.io/hostname",
										},
									},
								},
							},
						},
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-affinity-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-affinity-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone4",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Cell",
							Name:               "cell-affinity",
							UID:                "affinity-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(DefaultMultiOrchReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-affinity-multiorch",
							"app.kubernetes.io/component":  "multiorch",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "zone4",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-affinity-multiorch",
								"app.kubernetes.io/component":  "multiorch",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone4",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multiorch",
									Image:     DefaultMultiOrchImage,
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{Name: "http", ContainerPort: MultiOrchHTTPPort, Protocol: corev1.ProtocolTCP},
										{Name: "grpc", ContainerPort: MultiOrchGRPCPort, Protocol: corev1.ProtocolTCP},
									},
								},
							},
							Affinity: &corev1.Affinity{
								PodAntiAffinity: &corev1.PodAntiAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
										{
											Weight: 100,
											PodAffinityTerm: corev1.PodAffinityTerm{
												LabelSelector: &metav1.LabelSelector{
													MatchLabels: map[string]string{
														"app.kubernetes.io/component": "multiorch",
													},
												},
												TopologyKey: "kubernetes.io/hostname",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"with resource requirements": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-resources",
					Namespace: "default",
					UID:       "resources-uid",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone5",
					MultiOrch: multigresv1alpha1.StatelessSpec{
						ResourceRequirements: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1000m"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-resources-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-resources-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone5",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Cell",
							Name:               "cell-resources",
							UID:                "resources-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(DefaultMultiOrchReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-resources-multiorch",
							"app.kubernetes.io/component":  "multiorch",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "zone5",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-resources-multiorch",
								"app.kubernetes.io/component":  "multiorch",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone5",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "multiorch",
									Image: DefaultMultiOrchImage,
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("200m"),
											corev1.ResourceMemory: resource.MustParse("256Mi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1000m"),
											corev1.ResourceMemory: resource.MustParse("1Gi"),
										},
									},
									Ports: []corev1.ContainerPort{
										{Name: "http", ContainerPort: MultiOrchHTTPPort, Protocol: corev1.ProtocolTCP},
										{Name: "grpc", ContainerPort: MultiOrchGRPCPort, Protocol: corev1.ProtocolTCP},
									},
								},
							},
						},
					},
				},
			},
		},
		"invalid scheme - should error": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			scheme:  runtime.NewScheme(), // empty scheme
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildMultiOrchDeployment(tc.cell, tc.scheme)

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
		cell    *multigresv1alpha1.Cell
		scheme  *runtime.Scheme
		want    *corev1.Service
		wantErr bool
	}{
		"minimal spec": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell-multiorch",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cell-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Cell",
							Name:               "test-cell",
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
						"app.kubernetes.io/instance":   "test-cell-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
					},
					Ports: []corev1.ServicePort{
						{Name: "http", Port: MultiOrchHTTPPort, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
						{Name: "grpc", Port: MultiOrchGRPCPort, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP},
					},
				},
			},
		},
		"with different cell name": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-cell",
					Namespace: "prod-ns",
					UID:       "prod-uid",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "eu-central",
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-cell-multiorch",
					Namespace: "prod-ns",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "production-cell-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "eu-central",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Cell",
							Name:               "production-cell",
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
						"app.kubernetes.io/instance":   "production-cell-multiorch",
						"app.kubernetes.io/component":  "multiorch",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "eu-central",
					},
					Ports: []corev1.ServicePort{
						{Name: "http", Port: MultiOrchHTTPPort, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
						{Name: "grpc", Port: MultiOrchGRPCPort, TargetPort: intstr.FromString("grpc"), Protocol: corev1.ProtocolTCP},
					},
				},
			},
		},
		"invalid scheme - should error": {
			cell: &multigresv1alpha1.Cell{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cell",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.CellSpec{
					Name: "zone1",
				},
			},
			scheme:  runtime.NewScheme(), // empty scheme
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildMultiOrchService(tc.cell, tc.scheme)

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
