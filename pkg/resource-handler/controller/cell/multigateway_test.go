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

func TestBuildMultiGatewayDeployment(t *testing.T) {
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
					Name:      "test-cell-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cell-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
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
					Replicas: ptr.To(DefaultMultiGatewayReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-cell-multigateway",
							"app.kubernetes.io/component":  "multigateway",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-cell-multigateway",
								"app.kubernetes.io/component":  "multigateway",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multigateway",
									Image:     DefaultMultiGatewayImage,
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											ContainerPort: MultiGatewayHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "grpc",
											ContainerPort: MultiGatewayGRPCPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "postgres",
											ContainerPort: MultiGatewayPostgresPort,
											Protocol:      corev1.ProtocolTCP,
										},
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
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Replicas: ptr.To(int32(5)),
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-custom-replicas-multigateway",
					Namespace: "test-ns",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-custom-replicas-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
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
					Replicas: ptr.To(int32(5)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-custom-replicas-multigateway",
							"app.kubernetes.io/component":  "multigateway",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-custom-replicas-multigateway",
								"app.kubernetes.io/component":  "multigateway",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multigateway",
									Image:     DefaultMultiGatewayImage,
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											ContainerPort: MultiGatewayHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "grpc",
											ContainerPort: MultiGatewayGRPCPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "postgres",
											ContainerPort: MultiGatewayPostgresPort,
											Protocol:      corev1.ProtocolTCP,
										},
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
					Name:              "zone3",
					MultiGatewayImage: "custom/multigateway:v1.2.3",
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-custom-image-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-custom-image-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
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
					Replicas: ptr.To(DefaultMultiGatewayReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-custom-image-multigateway",
							"app.kubernetes.io/component":  "multigateway",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-custom-image-multigateway",
								"app.kubernetes.io/component":  "multigateway",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multigateway",
									Image:     "custom/multigateway:v1.2.3",
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											ContainerPort: MultiGatewayHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "grpc",
											ContainerPort: MultiGatewayGRPCPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "postgres",
											ContainerPort: MultiGatewayPostgresPort,
											Protocol:      corev1.ProtocolTCP,
										},
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
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Affinity: &corev1.Affinity{
							NodeAffinity: &corev1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
									NodeSelectorTerms: []corev1.NodeSelectorTerm{
										{
											MatchExpressions: []corev1.NodeSelectorRequirement{
												{
													Key:      "node-type",
													Operator: corev1.NodeSelectorOpIn,
													Values:   []string{"gateway"},
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
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-affinity-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-affinity-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
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
					Replicas: ptr.To(DefaultMultiGatewayReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-affinity-multigateway",
							"app.kubernetes.io/component":  "multigateway",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-affinity-multigateway",
								"app.kubernetes.io/component":  "multigateway",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:      "multigateway",
									Image:     DefaultMultiGatewayImage,
									Resources: corev1.ResourceRequirements{},
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											ContainerPort: MultiGatewayHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "grpc",
											ContainerPort: MultiGatewayGRPCPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "postgres",
											ContainerPort: MultiGatewayPostgresPort,
											Protocol:      corev1.ProtocolTCP,
										},
									},
								},
							},
							Affinity: &corev1.Affinity{
								NodeAffinity: &corev1.NodeAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
										NodeSelectorTerms: []corev1.NodeSelectorTerm{
											{
												MatchExpressions: []corev1.NodeSelectorRequirement{
													{
														Key:      "node-type",
														Operator: corev1.NodeSelectorOpIn,
														Values:   []string{"gateway"},
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
					MultiGateway: multigresv1alpha1.StatelessSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
				},
			},
			scheme: scheme,
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cell-resources-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "cell-resources-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
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
					Replicas: ptr.To(DefaultMultiGatewayReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "cell-resources-multigateway",
							"app.kubernetes.io/component":  "multigateway",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "cell-resources-multigateway",
								"app.kubernetes.io/component":  "multigateway",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "multigateway",
									Image: DefaultMultiGatewayImage,
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("128Mi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("500m"),
											corev1.ResourceMemory: resource.MustParse("512Mi"),
										},
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "http",
											ContainerPort: MultiGatewayHTTPPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "grpc",
											ContainerPort: MultiGatewayGRPCPort,
											Protocol:      corev1.ProtocolTCP,
										},
										{
											Name:          "postgres",
											ContainerPort: MultiGatewayPostgresPort,
											Protocol:      corev1.ProtocolTCP,
										},
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
			got, err := BuildMultiGatewayDeployment(tc.cell, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildMultiGatewayDeployment() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildMultiGatewayDeployment() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildMultiGatewayService(t *testing.T) {
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
					Name:      "test-cell-multigateway",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cell-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
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
						"app.kubernetes.io/instance":   "test-cell-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       MultiGatewayHTTPPort,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       MultiGatewayGRPCPort,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       MultiGatewayPostgresPort,
							TargetPort: intstr.FromString("postgres"),
							Protocol:   corev1.ProtocolTCP,
						},
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
					Name: "us-west",
				},
			},
			scheme: scheme,
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "production-cell-multigateway",
					Namespace: "prod-ns",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "production-cell-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
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
						"app.kubernetes.io/instance":   "production-cell-multigateway",
						"app.kubernetes.io/component":  "multigateway",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       MultiGatewayHTTPPort,
							TargetPort: intstr.FromString("http"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "grpc",
							Port:       MultiGatewayGRPCPort,
							TargetPort: intstr.FromString("grpc"),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "postgres",
							Port:       MultiGatewayPostgresPort,
							TargetPort: intstr.FromString("postgres"),
							Protocol:   corev1.ProtocolTCP,
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
			got, err := BuildMultiGatewayService(tc.cell, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildMultiGatewayService() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildMultiGatewayService() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
