package shard

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildPoolStatefulSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tests := map[string]struct {
		shard     *multigresv1alpha1.Shard
		pool      multigresv1alpha1.ShardPoolSpec
		poolIndex int
		scheme    *runtime.Scheme
		want      *appsv1.StatefulSet
		wantErr   bool
	}{
		"replica pool with default replicas": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
			poolIndex: 0,
			scheme:    scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard-replica",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-shard-replica",
						"app.kubernetes.io/component":  PoolComponentName,
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
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "test-shard-replica-headless",
					Replicas:    ptr.To(DefaultPoolReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "test-shard-replica",
							"app.kubernetes.io/component":  PoolComponentName,
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
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
								"app.kubernetes.io/instance":   "test-shard-replica",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.Shard{}),
								buildMultiPoolerSidecar(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Volumes: []corev1.Volume{
								buildPgctldVolume(),
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: DataVolumeName,
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
								VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
							},
						},
					},
				},
			},
		},
		"readOnly pool with custom replicas and cell": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-001",
					Namespace: "prod",
					UID:       "prod-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				Type:     "readOnly",
				Cell:     "zone-west",
				Replicas: ptr.To(int32(3)),
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
					StorageClassName: ptr.To("fast-ssd"),
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("20Gi"),
						},
					},
				},
			},
			poolIndex: 1,
			scheme:    scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-001-readOnly",
					Namespace: "prod",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-001-readOnly",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "shard-001",
							UID:                "prod-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "shard-001-readOnly-headless",
					Replicas:    ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "shard-001-readOnly",
							"app.kubernetes.io/component":  PoolComponentName,
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
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
								"app.kubernetes.io/instance":   "shard-001-readOnly",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.Shard{}),
								buildMultiPoolerSidecar(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Volumes: []corev1.Volume{
								buildPgctldVolume(),
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: DataVolumeName,
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								StorageClassName: ptr.To("fast-ssd"),
								AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("20Gi"),
									},
								},
								VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
							},
						},
					},
				},
			},
		},
		"pool without type uses pool index": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-002",
					Namespace: "default",
					UID:       "uid-002",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
			},
			poolIndex: 2,
			scheme:    scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-002-pool-2",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-002-pool-2",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "shard-002",
							UID:                "uid-002",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "shard-002-pool-2-headless",
					Replicas:    ptr.To(DefaultPoolReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "shard-002-pool-2",
							"app.kubernetes.io/component":  PoolComponentName,
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
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
								"app.kubernetes.io/instance":   "shard-002-pool-2",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.Shard{}),
								buildMultiPoolerSidecar(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Volumes: []corev1.Volume{
								buildPgctldVolume(),
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: DataVolumeName,
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("5Gi"),
									},
								},
								VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
							},
						},
					},
				},
			},
		},
		"pool with affinity": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-affinity",
					Namespace: "default",
					UID:       "affinity-uid",
				},
				Spec: multigresv1alpha1.ShardSpec{},
			},
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "disk-type",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"ssd"},
										},
									},
								},
							},
						},
					},
				},
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
			poolIndex: 0,
			scheme:    scheme,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-affinity-replica",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "shard-affinity-replica",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "multigres.com/v1alpha1",
							Kind:               "Shard",
							Name:               "shard-affinity",
							UID:                "affinity-uid",
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Spec: appsv1.StatefulSetSpec{
					ServiceName: "shard-affinity-replica-headless",
					Replicas:    ptr.To(DefaultPoolReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":       "multigres",
							"app.kubernetes.io/instance":   "shard-affinity-replica",
							"app.kubernetes.io/component":  PoolComponentName,
							"app.kubernetes.io/part-of":    "multigres",
							"app.kubernetes.io/managed-by": "multigres-operator",
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
								"app.kubernetes.io/instance":   "shard-affinity-replica",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
							},
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								buildPgctldInitContainer(&multigresv1alpha1.Shard{}),
								buildMultiPoolerSidecar(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Containers: []corev1.Container{
								buildPostgresContainer(&multigresv1alpha1.Shard{}, multigresv1alpha1.ShardPoolSpec{}),
							},
							Volumes: []corev1.Volume{
								buildPgctldVolume(),
							},
							Affinity: &corev1.Affinity{
								NodeAffinity: &corev1.NodeAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
										NodeSelectorTerms: []corev1.NodeSelectorTerm{
											{
												MatchExpressions: []corev1.NodeSelectorRequirement{
													{
														Key:      "disk-type",
														Operator: corev1.NodeSelectorOpIn,
														Values:   []string{"ssd"},
													},
												},
											},
										},
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
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
								VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
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
			pool: multigresv1alpha1.ShardPoolSpec{
				Type: "replica",
			},
			poolIndex: 0,
			scheme:    runtime.NewScheme(), // empty scheme
			wantErr:   true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := BuildPoolStatefulSet(tc.shard, tc.pool, tc.poolIndex, tc.scheme)

			if (err != nil) != tc.wantErr {
				t.Errorf("BuildPoolStatefulSet() error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildPoolStatefulSet() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPoolVolumeClaimTemplates(t *testing.T) {
	tests := map[string]struct {
		pool multigresv1alpha1.ShardPoolSpec
		want []corev1.PersistentVolumeClaim
	}{
		"with storage class and size": {
			pool: multigresv1alpha1.ShardPoolSpec{
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
					StorageClassName: ptr.To("fast-ssd"),
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
			want: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: DataVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To("fast-ssd"),
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
						VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
					},
				},
			},
		},
		"with volume mode already set": {
			pool: multigresv1alpha1.ShardPoolSpec{
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{
					VolumeMode: ptr.To(corev1.PersistentVolumeBlock),
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("20Gi"),
						},
					},
				},
			},
			want: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: DataVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						VolumeMode: ptr.To(corev1.PersistentVolumeBlock),
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("20Gi"),
							},
						},
					},
				},
			},
		},
		"minimal spec sets default VolumeMode": {
			pool: multigresv1alpha1.ShardPoolSpec{
				DataVolumeClaimTemplate: corev1.PersistentVolumeClaimSpec{},
			},
			want: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: DataVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildPoolVolumeClaimTemplates(tc.pool)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPoolVolumeClaimTemplates() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
