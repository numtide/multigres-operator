package multipooler

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

func stringPtr(s string) *string {
	return &s
}

func TestBuildStatefulSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		multipooler *multigresv1alpha1.MultiPooler
		scheme      *runtime.Scheme
		want        *appsv1.StatefulSet
		wantErr     bool
	}{
		"minimal spec - all defaults": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			scheme: scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
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
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "test-multipooler-headless",
					Replicas:    int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-multipooler",
							"app.kubernetes.io/component":  "multipooler",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "multigres-global-topo",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-multipooler",
								"app.kubernetes.io/component":  "multipooler",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "multigres-global-topo",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.MultiPooler{}),
								buildMultiPoolerSidecar(&multigresv1alpha1.MultiPooler{}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.MultiPooler{}),
							},
							Volumes: []corev1.Volume{
								{
									Name: PgctldVolumeName,
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: DataVolumeName,
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse(DefaultStorageSize),
									},
								},
							},
						},
					},
				},
			},
		},
		"custom replicas and cell name": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multipooler-custom",
					Namespace: "test",
					UID:       "custom-uid",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					Replicas: int32Ptr(3),
					CellName: "cell-1",
				},
			},
			scheme: scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multipooler-custom",
					Namespace: "test",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "multipooler-custom",
						"app.kubernetes.io/component":  "multipooler",
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "cell-1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "MultiPooler",
							Name:               "multipooler-custom",
							UID:                "custom-uid",
							Controller:         boolPtr(true),
							BlockOwnerDeletion: boolPtr(true),
						},
					},
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "multipooler-custom-headless",
					Replicas:    int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "multipooler-custom",
							"app.kubernetes.io/component":  "multipooler",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "cell-1",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "multipooler-custom",
								"app.kubernetes.io/component":  "multipooler",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "cell-1",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.MultiPooler{
									Spec: multigresv1alpha1.MultiPoolerSpec{
										CellName: "cell-1",
									},
								}),
								buildMultiPoolerSidecar(&multigresv1alpha1.MultiPooler{
									Spec: multigresv1alpha1.MultiPoolerSpec{
										CellName: "cell-1",
									},
								}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.MultiPooler{
									Spec: multigresv1alpha1.MultiPoolerSpec{
										CellName: "cell-1",
									},
								}),
							},
							Volumes: []corev1.Volume{
								{
									Name: PgctldVolumeName,
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: DataVolumeName,
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse(DefaultStorageSize),
									},
								},
							},
						},
					},
				},
			},
		},
		"custom storage size": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					StorageSize: "20Gi",
				},
			},
			scheme: scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
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
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "test-multipooler-headless",
					Replicas:    int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-multipooler",
							"app.kubernetes.io/component":  "multipooler",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "multigres-global-topo",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-multipooler",
								"app.kubernetes.io/component":  "multipooler",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "multigres-global-topo",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.MultiPooler{}),
								buildMultiPoolerSidecar(&multigresv1alpha1.MultiPooler{}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.MultiPooler{}),
							},
							Volumes: []corev1.Volume{
								{
									Name: PgctldVolumeName,
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: DataVolumeName,
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("20Gi"),
									},
								},
							},
						},
					},
				},
			},
		},
		"custom VolumeClaimTemplate": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{
					VolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteMany,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("100Gi"),
							},
						},
						StorageClassName: stringPtr("fast-ssd"),
					},
				},
			},
			scheme: scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
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
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "test-multipooler-headless",
					Replicas:    int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-multipooler",
							"app.kubernetes.io/component":  "multipooler",
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
							"multigres.com/cell":           "multigres-global-topo",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-multipooler",
								"app.kubernetes.io/component":  "multipooler",
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "multigres-global-topo",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.MultiPooler{}),
								buildMultiPoolerSidecar(&multigresv1alpha1.MultiPooler{}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.MultiPooler{}),
							},
							Volumes: []corev1.Volume{
								{
									Name: PgctldVolumeName,
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: DataVolumeName,
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteMany,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("100Gi"),
									},
								},
								StorageClassName: stringPtr("fast-ssd"),
							},
						},
					},
				},
			},
		},
		"scheme without MultiPooler type - should error": {
			multipooler: &multigresv1alpha1.MultiPooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multipooler",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.MultiPoolerSpec{},
			},
			scheme:  runtime.NewScheme(), // empty scheme without MultiPooler type
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildStatefulSet(tc.multipooler, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildStatefulSet() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildStatefulSet() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
