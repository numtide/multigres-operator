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
		shard    *multigresv1alpha1.Shard
		poolName string
		cellName string
		poolSpec multigresv1alpha1.PoolSpec
		scheme   *runtime.Scheme
		want     *appsv1.StatefulSet
		wantErr  bool
	}{
		"replica pool with default replicas": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
					UID:       "test-uid",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Type: "replica",
				Storage: multigresv1alpha1.StorageSpec{
					Size: "10Gi",
				},
			},
			poolName: "primary",
			cellName: "zone1",
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard-pool-primary-zone1",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cluster",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
						"multigres.com/database":       "testdb",
						"multigres.com/tablegroup":     "default",
						"multigres.com/cluster":        "test-cluster",
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
					ServiceName: "test-shard-pool-primary-zone1-headless",
					Replicas:    ptr.To(DefaultPoolReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance":  "test-cluster",
							"app.kubernetes.io/component": PoolComponentName,
							"multigres.com/cell":          "zone1",
							"multigres.com/database":      "testdb",
							"multigres.com/tablegroup":    "default",
							"multigres.com/cluster":       "test-cluster",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
						WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
						WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-cluster",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone1",
								"multigres.com/database":       "testdb",
								"multigres.com/tablegroup":     "default",
								"multigres.com/cluster":        "test-cluster",
							},
						},
						Spec: corev1.PodSpec{
							SecurityContext: &corev1.PodSecurityContext{
								FSGroup: ptr.To(int64(999)),
							},
							InitContainers: []corev1.Container{
								buildMultiPoolerSidecar(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{Name: "test-shard"},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									multigresv1alpha1.PoolSpec{
										Type: "replica",
										Storage: multigresv1alpha1.StorageSpec{
											Size: "10Gi",
										},
									},
									"primary",
									"zone1",
								),
							},
							Containers: []corev1.Container{
								{
									Name:  "postgres",
									Image: DefaultPgctldImage,
									Command: []string{
										"/usr/local/bin/pgctld",
									},
									Args: []string{
										"server",
										"--pooler-dir=/var/lib/pooler",
										"--grpc-port=15470",
										"--pg-port=5432",
										"--pg-listen-addresses=*",
										"--pg-database=postgres",
										"--pg-user=postgres",
										"--timeout=30",
										"--log-level=info",
										"--grpc-socket-file=/var/lib/pooler/pgctld.sock",
										"--pg-hba-template=/etc/pgctld/pg_hba_template.conf",
									},
									Env: []corev1.EnvVar{
										{
											Name:  "PGDATA",
											Value: "/var/lib/pooler/pg_data",
										},
										pgPasswordEnvVar(),
									},
									SecurityContext: &corev1.SecurityContext{
										RunAsUser:    ptr.To(int64(999)),
										RunAsGroup:   ptr.To(int64(999)),
										RunAsNonRoot: ptr.To(true),
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "pgdata",
											MountPath: "/var/lib/pooler",
										},
										{
											Name:      "backup-data",
											MountPath: "/backups",
										},
										{
											Name:      "socket-dir",
											MountPath: "/var/run/postgresql",
										},
										{
											Name:      "pg-hba-template",
											MountPath: "/etc/pgctld",
											ReadOnly:  true,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								buildBackupVolume(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "test-shard",
											Namespace: "default",
											UID:       "test-uid",
											Labels: map[string]string{
												"multigres.com/cluster": "test-cluster",
											},
										},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									"primary",
									"zone1",
								),
								buildSocketDirVolume(),
								buildPgHbaVolume(),
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
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
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
					Labels:    map[string]string{"multigres.com/cluster": "prod-cluster"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Type:            "readOnly",
				Cells:           []multigresv1alpha1.CellName{"zone-west"},
				ReplicasPerCell: ptr.To(int32(3)),
				Storage: multigresv1alpha1.StorageSpec{
					Class: "fast-ssd",
					Size:  "20Gi",
				},
			},
			poolName: "replica",
			cellName: "zone-west",
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-001-pool-replica-zone-west",
					Namespace: "prod",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "prod-cluster",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone-west",
						"multigres.com/database":       "testdb",
						"multigres.com/tablegroup":     "default",
						"multigres.com/cluster":        "prod-cluster",
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
					ServiceName: "shard-001-pool-replica-zone-west-headless",
					Replicas:    ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance":  "prod-cluster",
							"app.kubernetes.io/component": PoolComponentName,
							"multigres.com/cell":          "zone-west",
							"multigres.com/database":      "testdb",
							"multigres.com/tablegroup":    "default",
							"multigres.com/cluster":       "prod-cluster",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
						WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
						WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "prod-cluster",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone-west",
								"multigres.com/database":       "testdb",
								"multigres.com/tablegroup":     "default",
								"multigres.com/cluster":        "prod-cluster",
							},
						},
						Spec: corev1.PodSpec{
							SecurityContext: &corev1.PodSecurityContext{
								FSGroup: ptr.To(int64(999)),
							},
							InitContainers: []corev1.Container{
								buildMultiPoolerSidecar(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{
											Name: "shard-001",
											Labels: map[string]string{
												"multigres.com/cluster": "prod-cluster",
											},
										},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									multigresv1alpha1.PoolSpec{
										Type:            "readOnly",
										Cells:           []multigresv1alpha1.CellName{"zone-west"},
										ReplicasPerCell: ptr.To(int32(3)),
										Storage: multigresv1alpha1.StorageSpec{
											Class: "fast-ssd",
											Size:  "20Gi",
										},
									},
									"replica",
									"zone-west",
								),
							},
							Containers: []corev1.Container{
								{
									Name:  "postgres",
									Image: DefaultPgctldImage,
									Command: []string{
										"/usr/local/bin/pgctld",
									},
									Args: []string{
										"server",
										"--pooler-dir=/var/lib/pooler",
										"--grpc-port=15470",
										"--pg-port=5432",
										"--pg-listen-addresses=*",
										"--pg-database=postgres",
										"--pg-user=postgres",
										"--timeout=30",
										"--log-level=info",
										"--grpc-socket-file=/var/lib/pooler/pgctld.sock",
										"--pg-hba-template=/etc/pgctld/pg_hba_template.conf",
									},
									Env: []corev1.EnvVar{
										{
											Name:  "PGDATA",
											Value: "/var/lib/pooler/pg_data",
										},
										pgPasswordEnvVar(),
									},
									SecurityContext: &corev1.SecurityContext{
										RunAsUser:    ptr.To(int64(999)),
										RunAsGroup:   ptr.To(int64(999)),
										RunAsNonRoot: ptr.To(true),
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "pgdata",
											MountPath: "/var/lib/pooler",
										},
										{
											Name:      "backup-data",
											MountPath: "/backups",
										},
										{
											Name:      "socket-dir",
											MountPath: "/var/run/postgresql",
										},
										{
											Name:      "pg-hba-template",
											MountPath: "/etc/pgctld",
											ReadOnly:  true,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								buildBackupVolume(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "shard-001",
											Namespace: "prod",
											UID:       "prod-uid",
											Labels: map[string]string{
												"multigres.com/cluster": "prod-cluster",
											},
										},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									"replica",
									"zone-west",
								),
								buildSocketDirVolume(),
								buildPgHbaVolume(),
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
		"pool without type uses pool index": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-002",
					Namespace: "default",
					UID:       "uid-002",
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Storage: multigresv1alpha1.StorageSpec{
					Size: "5Gi",
				},
			},
			poolName: "readOnly",
			cellName: "zone1",
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-002-pool-readOnly-zone1",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cluster",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
						"multigres.com/database":       "testdb",
						"multigres.com/tablegroup":     "default",
						"multigres.com/cluster":        "test-cluster",
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
					ServiceName: "shard-002-pool-readOnly-zone1-headless",
					Replicas:    ptr.To(DefaultPoolReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance":  "test-cluster",
							"app.kubernetes.io/component": PoolComponentName,
							"multigres.com/cell":          "zone1",
							"multigres.com/database":      "testdb",
							"multigres.com/tablegroup":    "default",
							"multigres.com/cluster":       "test-cluster",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
						WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
						WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-cluster",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone1",
								"multigres.com/database":       "testdb",
								"multigres.com/tablegroup":     "default",
								"multigres.com/cluster":        "test-cluster",
							},
						},
						Spec: corev1.PodSpec{
							SecurityContext: &corev1.PodSecurityContext{
								FSGroup: ptr.To(int64(999)),
							},
							InitContainers: []corev1.Container{
								buildMultiPoolerSidecar(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{
											Name: "shard-002",
											Labels: map[string]string{
												"multigres.com/cluster": "test-cluster",
											},
										},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									multigresv1alpha1.PoolSpec{
										Storage: multigresv1alpha1.StorageSpec{
											Size: "5Gi",
										},
									},
									"readOnly",
									"zone1",
								),
							},
							Containers: []corev1.Container{
								{
									Name:  "postgres",
									Image: DefaultPgctldImage,
									Command: []string{
										"/usr/local/bin/pgctld",
									},
									Args: []string{
										"server",
										"--pooler-dir=/var/lib/pooler",
										"--grpc-port=15470",
										"--pg-port=5432",
										"--pg-listen-addresses=*",
										"--pg-database=postgres",
										"--pg-user=postgres",
										"--timeout=30",
										"--log-level=info",
										"--grpc-socket-file=/var/lib/pooler/pgctld.sock",
										"--pg-hba-template=/etc/pgctld/pg_hba_template.conf",
									},
									Env: []corev1.EnvVar{
										{
											Name:  "PGDATA",
											Value: "/var/lib/pooler/pg_data",
										},
										pgPasswordEnvVar(),
									},
									SecurityContext: &corev1.SecurityContext{
										RunAsUser:    ptr.To(int64(999)),
										RunAsGroup:   ptr.To(int64(999)),
										RunAsNonRoot: ptr.To(true),
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "pgdata",
											MountPath: "/var/lib/pooler",
										},
										{
											Name:      "backup-data",
											MountPath: "/backups",
										},
										{
											Name:      "socket-dir",
											MountPath: "/var/run/postgresql",
										},
										{
											Name:      "pg-hba-template",
											MountPath: "/etc/pgctld",
											ReadOnly:  true,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								buildBackupVolume(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "shard-002",
											Namespace: "default",
											UID:       "uid-002",
											Labels: map[string]string{
												"multigres.com/cluster": "test-cluster",
											},
										},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									"readOnly",
									"zone1",
								),
								buildSocketDirVolume(),
								buildPgHbaVolume(),
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
										corev1.ResourceStorage: resource.MustParse("5Gi"),
									},
								},
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
					Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
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
				Storage: multigresv1alpha1.StorageSpec{
					Size: "10Gi",
				},
			},
			poolName: "primary",
			cellName: "zone1",
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shard-affinity-pool-primary-zone1",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cluster",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
						"multigres.com/database":       "testdb",
						"multigres.com/tablegroup":     "default",
						"multigres.com/cluster":        "test-cluster",
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
					ServiceName: "shard-affinity-pool-primary-zone1-headless",
					Replicas:    ptr.To(DefaultPoolReplicas),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance":  "test-cluster",
							"app.kubernetes.io/component": PoolComponentName,
							"multigres.com/cell":          "zone1",
							"multigres.com/database":      "testdb",
							"multigres.com/tablegroup":    "default",
							"multigres.com/cluster":       "test-cluster",
						},
					},
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.RollingUpdateStatefulSetStrategyType,
					},
					PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
						WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
						WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":       "multigres",
								"app.kubernetes.io/instance":   "test-cluster",
								"app.kubernetes.io/component":  PoolComponentName,
								"app.kubernetes.io/part-of":    "multigres",
								"app.kubernetes.io/managed-by": "multigres-operator",
								"multigres.com/cell":           "zone1",
								"multigres.com/database":       "testdb",
								"multigres.com/tablegroup":     "default",
								"multigres.com/cluster":        "test-cluster",
							},
						},
						Spec: corev1.PodSpec{
							SecurityContext: &corev1.PodSecurityContext{
								FSGroup: ptr.To(int64(999)),
							},
							InitContainers: []corev1.Container{
								buildMultiPoolerSidecar(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{
											Name: "shard-affinity",
											Labels: map[string]string{
												"multigres.com/cluster": "test-cluster",
											},
										},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									multigresv1alpha1.PoolSpec{
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
										Storage: multigresv1alpha1.StorageSpec{
											Size: "10Gi",
										},
									},
									"primary",
									"zone1",
								),
							},
							Containers: []corev1.Container{
								{
									Name:  "postgres",
									Image: DefaultPgctldImage,
									Command: []string{
										"/usr/local/bin/pgctld",
									},
									Args: []string{
										"server",
										"--pooler-dir=/var/lib/pooler",
										"--grpc-port=15470",
										"--pg-port=5432",
										"--pg-listen-addresses=*",
										"--pg-database=postgres",
										"--pg-user=postgres",
										"--timeout=30",
										"--log-level=info",
										"--grpc-socket-file=/var/lib/pooler/pgctld.sock",
										"--pg-hba-template=/etc/pgctld/pg_hba_template.conf",
									},
									Env: []corev1.EnvVar{
										{
											Name:  "PGDATA",
											Value: "/var/lib/pooler/pg_data",
										},
										pgPasswordEnvVar(),
									},
									SecurityContext: &corev1.SecurityContext{
										RunAsUser:    ptr.To(int64(999)),
										RunAsGroup:   ptr.To(int64(999)),
										RunAsNonRoot: ptr.To(true),
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "pgdata",
											MountPath: "/var/lib/pooler",
										},
										{
											Name:      "backup-data",
											MountPath: "/backups",
										},
										{
											Name:      "socket-dir",
											MountPath: "/var/run/postgresql",
										},
										{
											Name:      "pg-hba-template",
											MountPath: "/etc/pgctld",
											ReadOnly:  true,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								buildBackupVolume(
									&multigresv1alpha1.Shard{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "shard-affinity",
											Namespace: "default",
											UID:       "affinity-uid",
											Labels: map[string]string{
												"multigres.com/cluster": "test-cluster",
											},
										},
										Spec: multigresv1alpha1.ShardSpec{
											DatabaseName:   "testdb",
											TableGroupName: "default",
										},
									},
									"primary",
									"zone1",
								),
								buildSocketDirVolume(),
								buildPgHbaVolume(),
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
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("10Gi"),
									},
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
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
				},
			},
			poolSpec: multigresv1alpha1.PoolSpec{
				Type: "replica",
			},
			poolName: "primary",
			cellName: "zone1",
			scheme:   runtime.NewScheme(), // empty scheme
			wantErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if tc.want != nil {
				hashedName := buildHashedPoolName(tc.shard, tc.poolName, tc.cellName)
				hashedSvcName := buildPoolHeadlessServiceName(tc.shard, tc.poolName, tc.cellName)
				hashedBackupPVC := buildHashedBackupPVCName(tc.shard, tc.poolName, tc.cellName)

				tc.want.Name = hashedName
				tc.want.Spec.ServiceName = hashedSvcName

				if tc.want.Labels != nil {
					tc.want.Labels["multigres.com/pool"] = tc.poolName
				}
				if tc.want.Spec.Selector != nil && tc.want.Spec.Selector.MatchLabels != nil {
					tc.want.Spec.Selector.MatchLabels["multigres.com/pool"] = tc.poolName
				}
				if tc.want.Spec.Template.Labels != nil {
					tc.want.Spec.Template.Labels["multigres.com/pool"] = tc.poolName
				}

				for i, vol := range tc.want.Spec.Template.Spec.Volumes {
					if vol.Name == "backup-data-vol" && vol.PersistentVolumeClaim != nil {
						tc.want.Spec.Template.Spec.Volumes[i].PersistentVolumeClaim.ClaimName = hashedBackupPVC
					}
				}
			}

			testScheme := scheme
			if tc.scheme != nil {
				testScheme = tc.scheme
			}
			got, err := BuildPoolStatefulSet(
				tc.shard,
				tc.poolName,
				tc.cellName,
				tc.poolSpec,
				testScheme,
			)

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
		poolSpec multigresv1alpha1.PoolSpec
		want     []corev1.PersistentVolumeClaim
	}{
		"with storage class and size": {
			poolSpec: multigresv1alpha1.PoolSpec{
				Storage: multigresv1alpha1.StorageSpec{
					Class: "fast-ssd",
					Size:  "10Gi",
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
					},
				},
			},
		},
		"with size": {
			poolSpec: multigresv1alpha1.PoolSpec{
				Storage: multigresv1alpha1.StorageSpec{
					Size: "20Gi",
				},
			},
			want: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: DataVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("20Gi"),
							},
						},
					},
				},
			},
		},
		"minimal spec uses defaults": {
			poolSpec: multigresv1alpha1.PoolSpec{},
			want: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: DataVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(DefaultDataVolumeSize),
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildPoolVolumeClaimTemplates(tc.poolSpec)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildPoolVolumeClaimTemplates() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildBackupPVC(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			UID:       "test-uid",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
	}

	tests := map[string]struct {
		poolSpec multigresv1alpha1.PoolSpec
		want     *corev1.PersistentVolumeClaim
	}{
		"defaults": {
			poolSpec: multigresv1alpha1.PoolSpec{},
			want: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-data-test-shard-pool-primary-zone1",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cluster",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
						"multigres.com/database":       "",
						"multigres.com/tablegroup":     "",
						"multigres.com/cluster":        "test-cluster",
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
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
		},
		"inherit storage class": {
			poolSpec: multigresv1alpha1.PoolSpec{
				Storage: multigresv1alpha1.StorageSpec{
					Class: "fast-ssd",
				},
			},
			want: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-data-test-shard-pool-primary-zone1",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cluster",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
						"multigres.com/database":       "",
						"multigres.com/tablegroup":     "",
						"multigres.com/cluster":        "test-cluster",
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
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: ptr.To("fast-ssd"),
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
		},
		"override backup storage": {
			poolSpec: multigresv1alpha1.PoolSpec{
				Storage: multigresv1alpha1.StorageSpec{
					Class: "fast-ssd",
				},
				BackupStorage: multigresv1alpha1.StorageSpec{
					Class:       "backup-nfs",
					Size:        "100Gi",
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
				},
			},
			want: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-data-test-shard-pool-primary-zone1",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":       "multigres",
						"app.kubernetes.io/instance":   "test-cluster",
						"app.kubernetes.io/component":  PoolComponentName,
						"app.kubernetes.io/part-of":    "multigres",
						"app.kubernetes.io/managed-by": "multigres-operator",
						"multigres.com/cell":           "zone1",
						"multigres.com/database":       "",
						"multigres.com/tablegroup":     "",
						"multigres.com/cluster":        "test-cluster",
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
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: ptr.To("backup-nfs"),
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"),
						},
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if tc.want != nil {
				hashedPVCName := buildHashedBackupPVCName(shard, "primary", "zone1")
				tc.want.Name = hashedPVCName
				if tc.want.Labels != nil {
					tc.want.Labels["multigres.com/pool"] = "primary"
				}
			}

			testScheme := scheme
			if name == "error on controller reference" {
				testScheme = runtime.NewScheme() // Empty scheme triggers SetControllerReference error
			}

			got, err := BuildBackupPVC(
				shard,
				"primary",
				"zone1",
				tc.poolSpec,
				testScheme,
			)

			if name == "error on controller reference" {
				if err == nil {
					t.Error("BuildBackupPVC() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("BuildBackupPVC() error = %v", err)
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildBackupPVC() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildBackupPVC_Error(t *testing.T) {
	// Separate test for error case to avoid messing with table driven test logic too much
	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}
	// Empty scheme causes SetControllerReference to fail
	_, err := BuildBackupPVC(
		shard,
		"pool",
		"cell",
		multigresv1alpha1.PoolSpec{},
		runtime.NewScheme(),
	)
	if err == nil {
		t.Error("BuildBackupPVC() expected error with empty scheme, got nil")
	}
}
