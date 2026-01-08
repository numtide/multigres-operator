//go:build integration
// +build integration

package shard_test

import (
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	shardcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/shard"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestSetupWithManager(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	mgr := testutil.SetUpEnvtestManager(t, scheme,
		testutil.WithCRDPaths(
			filepath.Join("../../../../", "config", "crd", "bases"),
		),
	)

	if err := (&shardcontroller.ShardReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, controller.Options{
		SkipNameValidation: ptr.To(true),
	}); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

func TestShardReconciliation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		shard         *multigresv1alpha1.Shard
		wantResources []client.Object
	}{
		"simple shard with single replica pool": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					ShardName:      "0",
					Images: multigresv1alpha1.ShardImages{
						MultiOrch:   "ghcr.io/multigres/multigres:main",
						MultiPooler: "ghcr.io/multigres/multigres:main",
						Postgres:    "postgres:17",
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd2",
					},
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"us-west-1a", "us-west-1b"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"us-west-1a"},
							Type:            "readWrite",
							ReplicasPerCell: ptr.To(int32(2)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			wantResources: []client.Object{
				// MultiOrch Deployment for us-west-1a
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-shard-multiorch-us-west-1a",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-multiorch-us-west-1a", "multiorch", "us-west-1a"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "test-shard-multiorch-us-west-1a", "multiorch", "us-west-1a"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "test-shard-multiorch-us-west-1a", "multiorch", "us-west-1a"),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "multiorch",
										Image: "ghcr.io/multigres/multigres:main",
										Args: []string{
											"multiorch",
											"--http-port", "15300",
											"--grpc-port", "15370",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "us-west-1a",
											"--watch-targets", "postgres",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15300),
											tcpPort(t, "grpc", 15370),
										},
									},
								},
							},
						},
					},
				},
				// MultiOrch Service for us-west-1a
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-shard-multiorch-us-west-1a",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-multiorch-us-west-1a", "multiorch", "us-west-1a"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15300),
							tcpServicePort(t, "grpc", 15370),
						},
						Selector: shardLabels(t, "test-shard-multiorch-us-west-1a", "multiorch", "us-west-1a"),
					},
				},
				// MultiOrch Deployment for us-west-1b
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-shard-multiorch-us-west-1b",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-multiorch-us-west-1b", "multiorch", "us-west-1b"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "test-shard-multiorch-us-west-1b", "multiorch", "us-west-1b"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "test-shard-multiorch-us-west-1b", "multiorch", "us-west-1b"),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "multiorch",
										Image: "ghcr.io/multigres/multigres:main",
										Args: []string{
											"multiorch",
											"--http-port", "15300",
											"--grpc-port", "15370",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "us-west-1b",
											"--watch-targets", "postgres",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15300),
											tcpPort(t, "grpc", 15370),
										},
									},
								},
							},
						},
					},
				},
				// MultiOrch Service for us-west-1b
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-shard-multiorch-us-west-1b",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-multiorch-us-west-1b", "multiorch", "us-west-1b"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15300),
							tcpServicePort(t, "grpc", 15370),
						},
						Selector: shardLabels(t, "test-shard-multiorch-us-west-1b", "multiorch", "us-west-1b"),
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-shard-pool-primary-us-west-1a",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-pool-primary-us-west-1a", "shard-pool", "us-west-1a"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: appsv1.StatefulSetSpec{
						ServiceName: "test-shard-pool-primary-us-west-1a-headless",
						Replicas:    ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "test-shard-pool-primary-us-west-1a", "shard-pool", "us-west-1a"),
						},
						PodManagementPolicy: appsv1.ParallelPodManagement,
						UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
							Type: appsv1.RollingUpdateStatefulSetStrategyType,
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "test-shard-pool-primary-us-west-1a", "shard-pool", "us-west-1a"),
							},
							Spec: corev1.PodSpec{
								InitContainers: []corev1.Container{
									// ALTERNATIVE: Uncomment for binary-copy approach
									// {
									// 	Name:    "pgctld-init",
									// 	Image:   "ghcr.io/multigres/pgctld:main",
									// 	Command: []string{"/bin/sh", "-c"},
									// 	Args: []string{
									// 		"cp /usr/local/bin/pgctld /shared/pgctld",
									// 	},
									// 	VolumeMounts: []corev1.VolumeMount{
									// 		{Name: "pgctld-bin", MountPath: "/shared"},
									// 	},
									// },
									{
										Name:  "multipooler",
										Image: "ghcr.io/multigres/multigres:main",
										Args: []string{
											"multipooler",
											"--http-port", "15200",
											"--grpc-port", "15270",
											"--pooler-dir", "/var/lib/pooler",
											"--socket-file", "/var/lib/pooler/pg_sockets/.s.PGSQL.5432",
											"--service-map", "grpc-pooler",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "us-west-1a",
											"--database", "testdb",
											"--table-group", "default",
											"--shard", "0",
											"--service-id", "$(POD_NAME)",
											"--pgctld-addr", "localhost:15470",
											"--pg-port", "5432",
										},
										Ports:         multipoolerPorts(t),
										RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
										SecurityContext: &corev1.SecurityContext{
											RunAsUser:    ptr.To(int64(999)),
											RunAsGroup:   ptr.To(int64(999)),
											RunAsNonRoot: ptr.To(true),
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgdata", MountPath: "/var/lib/pooler"},
											{Name: "backup-data", MountPath: "/backups"},
										},
									},
								},
								Containers: []corev1.Container{
									{
										Name:    "postgres",
										Image:   "postgres:17",
										Command: []string{"/usr/local/bin/pgctld"},
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
										},
										Env: []corev1.EnvVar{
											{Name: "PGDATA", Value: "/var/lib/pooler/pg_data"},
										},
										SecurityContext: &corev1.SecurityContext{
											RunAsUser:    ptr.To(int64(999)),
											RunAsGroup:   ptr.To(int64(999)),
											RunAsNonRoot: ptr.To(true),
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgdata", MountPath: "/var/lib/pooler"},
											// ALTERNATIVE: Uncomment for binary-copy approach
											// {Name: "pgctld-bin", MountPath: "/usr/local/bin/multigres"},
											{Name: "backup-data", MountPath: "/backups"},
										},
									},
								},
								Volumes: []corev1.Volume{
									// ALTERNATIVE: Uncomment for binary-copy approach
									// {
									// 	Name: "pgctld-bin",
									// 	VolumeSource: corev1.VolumeSource{
									// 		EmptyDir: &corev1.EmptyDirVolumeSource{},
									// 	},
									// },
									{
										Name: "backup-data",
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
									Name: "pgdata",
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
								Status: corev1.PersistentVolumeClaimStatus{
									Phase: corev1.ClaimPending,
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-shard-pool-primary-us-west-1a-headless",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-pool-primary-us-west-1a", "shard-pool", "us-west-1a"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: corev1.ClusterIPNone,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15200),
							tcpServicePort(t, "grpc", 15270),
							tcpServicePort(t, "postgres", 5432),
						},
						Selector:                 shardLabels(t, "test-shard-pool-primary-us-west-1a", "shard-pool", "us-west-1a"),
						PublishNotReadyAddresses: true,
					},
				},
			},
		},
		"shard with pool spanning two cells": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-cell-shard",
					Namespace: "default",
				},
				Spec: multigresv1alpha1.ShardSpec{
					DatabaseName:   "testdb",
					TableGroupName: "default",
					ShardName:      "0",
					Images: multigresv1alpha1.ShardImages{
						MultiOrch:   "ghcr.io/multigres/multigres:main",
						MultiPooler: "ghcr.io/multigres/multigres:main",
						Postgres:    "postgres:17",
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:        "global-topo:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd2",
					},
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"zone1", "zone2"},
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:           []multigresv1alpha1.CellName{"zone1", "zone2"},
							Type:            "readWrite",
							ReplicasPerCell: ptr.To(int32(2)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			wantResources: []client.Object{
				// MultiOrch Deployment for zone1
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-multiorch-zone1",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-multiorch-zone1", "multiorch", "zone1"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "multi-cell-shard-multiorch-zone1", "multiorch", "zone1"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "multi-cell-shard-multiorch-zone1", "multiorch", "zone1"),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "multiorch",
										Image: "ghcr.io/multigres/multigres:main",
										Args: []string{
											"multiorch",
											"--http-port", "15300",
											"--grpc-port", "15370",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "zone1",
											"--watch-targets", "postgres",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15300),
											tcpPort(t, "grpc", 15370),
										},
									},
								},
							},
						},
					},
				},
				// MultiOrch Service for zone1
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-multiorch-zone1",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-multiorch-zone1", "multiorch", "zone1"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15300),
							tcpServicePort(t, "grpc", 15370),
						},
						Selector: shardLabels(t, "multi-cell-shard-multiorch-zone1", "multiorch", "zone1"),
					},
				},
				// MultiOrch Deployment for zone2
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-multiorch-zone2",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-multiorch-zone2", "multiorch", "zone2"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "multi-cell-shard-multiorch-zone2", "multiorch", "zone2"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "multi-cell-shard-multiorch-zone2", "multiorch", "zone2"),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "multiorch",
										Image: "ghcr.io/multigres/multigres:main",
										Args: []string{
											"multiorch",
											"--http-port", "15300",
											"--grpc-port", "15370",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "zone2",
											"--watch-targets", "postgres",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15300),
											tcpPort(t, "grpc", 15370),
										},
									},
								},
							},
						},
					},
				},
				// MultiOrch Service for zone2
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-multiorch-zone2",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-multiorch-zone2", "multiorch", "zone2"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15300),
							tcpServicePort(t, "grpc", 15370),
						},
						Selector: shardLabels(t, "multi-cell-shard-multiorch-zone2", "multiorch", "zone2"),
					},
				},
				// StatefulSet for zone1
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-pool-primary-zone1",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-pool-primary-zone1", "shard-pool", "zone1"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: appsv1.StatefulSetSpec{
						ServiceName: "multi-cell-shard-pool-primary-zone1-headless",
						Replicas:    ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "multi-cell-shard-pool-primary-zone1", "shard-pool", "zone1"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "multi-cell-shard-pool-primary-zone1", "shard-pool", "zone1"),
							},
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{
									FSGroup: ptr.To(int64(999)),
								},
								Volumes: []corev1.Volume{
									// ALTERNATIVE: Uncomment for binary-copy approach
									// {
									// 	Name: "pgctld-bin",
									// 	VolumeSource: corev1.VolumeSource{
									// 		EmptyDir: &corev1.EmptyDirVolumeSource{},
									// 	},
									// },
									{
										Name: "backup-data",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
								},
								InitContainers: []corev1.Container{
									{
										Name:  "multipooler",
										Image: "ghcr.io/multigres/multigres:main",
										Args: []string{
											"multipooler",
											"--http-port", "15200",
											"--grpc-port", "15270",
											"--pooler-dir", "/var/lib/pooler",
											"--socket-file", "/var/lib/pooler/pg_sockets/.s.PGSQL.5432",
											"--service-map", "grpc-pooler",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "zone1",
											"--database", "testdb",
											"--table-group", "default",
											"--shard", "0",
											"--service-id", "$(POD_NAME)",
											"--pgctld-addr", "localhost:15470",
											"--pg-port", "5432",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15200),
											tcpPort(t, "grpc", 15270),
											tcpPort(t, "postgres", 5432),
										},
										RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
										SecurityContext: &corev1.SecurityContext{
											RunAsUser:    ptr.To(int64(999)),
											RunAsGroup:   ptr.To(int64(999)),
											RunAsNonRoot: ptr.To(true),
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgdata", MountPath: "/var/lib/pooler"},
											{Name: "backup-data", MountPath: "/backups"},
										},
									},
								},
								Containers: []corev1.Container{
									{
										Name:    "postgres",
										Image:   "postgres:17",
										Command: []string{"/usr/local/bin/pgctld"},
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
										},
										Env: []corev1.EnvVar{
											{Name: "PGDATA", Value: "/var/lib/pooler/pg_data"},
										},
										SecurityContext: &corev1.SecurityContext{
											RunAsUser:    ptr.To(int64(999)),
											RunAsGroup:   ptr.To(int64(999)),
											RunAsNonRoot: ptr.To(true),
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgdata", MountPath: "/var/lib/pooler"},
											// ALTERNATIVE: Uncomment for binary-copy approach
											// {Name: "pgctld-bin", MountPath: "/usr/local/bin/multigres"},
											{Name: "backup-data", MountPath: "/backups"},
										},
									},
								},
							},
						},
						VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{Name: "pgdata"},
								Spec: corev1.PersistentVolumeClaimSpec{
									AccessModes: []corev1.PersistentVolumeAccessMode{
										corev1.ReadWriteOnce,
									},
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: resource.MustParse("10Gi"),
										},
									},
									VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
								},
								Status: corev1.PersistentVolumeClaimStatus{
									Phase: corev1.ClaimPending,
								},
							},
						},
					},
				},
				// Headless Service for zone1
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-pool-primary-zone1-headless",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-pool-primary-zone1", "shard-pool", "zone1"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: corev1.ClusterIPNone,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15200),
							tcpServicePort(t, "grpc", 15270),
							tcpServicePort(t, "postgres", 5432),
						},
						Selector:                 shardLabels(t, "multi-cell-shard-pool-primary-zone1", "shard-pool", "zone1"),
						PublishNotReadyAddresses: true,
					},
				},
				// StatefulSet for zone2
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-pool-primary-zone2",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-pool-primary-zone2", "shard-pool", "zone2"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: appsv1.StatefulSetSpec{
						ServiceName: "multi-cell-shard-pool-primary-zone2-headless",
						Replicas:    ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "multi-cell-shard-pool-primary-zone2", "shard-pool", "zone2"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "multi-cell-shard-pool-primary-zone2", "shard-pool", "zone2"),
							},
							Spec: corev1.PodSpec{
								SecurityContext: &corev1.PodSecurityContext{
									FSGroup: ptr.To(int64(999)),
								},
								Volumes: []corev1.Volume{
									// ALTERNATIVE: Uncomment for binary-copy approach
									// {
									// 	Name: "pgctld-bin",
									// 	VolumeSource: corev1.VolumeSource{
									// 		EmptyDir: &corev1.EmptyDirVolumeSource{},
									// 	},
									// },
									{
										Name: "backup-data",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
								},
								InitContainers: []corev1.Container{
									{
										Name:  "multipooler",
										Image: "ghcr.io/multigres/multigres:main",
										Args: []string{
											"multipooler",
											"--http-port", "15200",
											"--grpc-port", "15270",
											"--pooler-dir", "/var/lib/pooler",
											"--socket-file", "/var/lib/pooler/pg_sockets/.s.PGSQL.5432",
											"--service-map", "grpc-pooler",
											"--topo-global-server-addresses", "global-topo:2379",
											"--topo-global-root", "/multigres/global",
											"--cell", "zone2",
											"--database", "testdb",
											"--table-group", "default",
											"--shard", "0",
											"--service-id", "$(POD_NAME)",
											"--pgctld-addr", "localhost:15470",
											"--pg-port", "5432",
										},
										Ports: []corev1.ContainerPort{
											tcpPort(t, "http", 15200),
											tcpPort(t, "grpc", 15270),
											tcpPort(t, "postgres", 5432),
										},
										RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
										SecurityContext: &corev1.SecurityContext{
											RunAsUser:    ptr.To(int64(999)),
											RunAsGroup:   ptr.To(int64(999)),
											RunAsNonRoot: ptr.To(true),
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgdata", MountPath: "/var/lib/pooler"},
											{Name: "backup-data", MountPath: "/backups"},
										},
									},
								},
								Containers: []corev1.Container{
									{
										Name:    "postgres",
										Image:   "postgres:17",
										Command: []string{"/usr/local/bin/pgctld"},
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
										},
										Env: []corev1.EnvVar{
											{Name: "PGDATA", Value: "/var/lib/pooler/pg_data"},
										},
										SecurityContext: &corev1.SecurityContext{
											RunAsUser:    ptr.To(int64(999)),
											RunAsGroup:   ptr.To(int64(999)),
											RunAsNonRoot: ptr.To(true),
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgdata", MountPath: "/var/lib/pooler"},
											// ALTERNATIVE: Uncomment for binary-copy approach
											// {Name: "pgctld-bin", MountPath: "/usr/local/bin/multigres"},
											{Name: "backup-data", MountPath: "/backups"},
										},
									},
								},
							},
						},
						VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{Name: "pgdata"},
								Spec: corev1.PersistentVolumeClaimSpec{
									AccessModes: []corev1.PersistentVolumeAccessMode{
										corev1.ReadWriteOnce,
									},
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: resource.MustParse("10Gi"),
										},
									},
									VolumeMode: ptr.To(corev1.PersistentVolumeFilesystem),
								},
								Status: corev1.PersistentVolumeClaimStatus{
									Phase: corev1.ClaimPending,
								},
							},
						},
					},
				},
				// Headless Service for zone2
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "multi-cell-shard-pool-primary-zone2-headless",
						Namespace:       "default",
						Labels:          shardLabels(t, "multi-cell-shard-pool-primary-zone2", "shard-pool", "zone2"),
						OwnerReferences: shardOwnerRefs(t, "multi-cell-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: corev1.ClusterIPNone,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15200),
							tcpServicePort(t, "grpc", 15270),
							tcpServicePort(t, "postgres", 5432),
						},
						Selector:                 shardLabels(t, "multi-cell-shard-pool-primary-zone2", "shard-pool", "zone2"),
						PublishNotReadyAddresses: true,
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			mgr := testutil.SetUpEnvtestManager(t, scheme,
				testutil.WithCRDPaths(
					filepath.Join("../../../../", "config", "crd", "bases"),
				),
			)

			watcher := testutil.NewResourceWatcher(t, ctx, mgr,
				testutil.WithCmpOpts(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreServiceRuntimeFields(),
					testutil.IgnoreDeploymentRuntimeFields(),
					testutil.IgnoreStatefulSetRuntimeFields(),
					testutil.IgnorePodSpecDefaults(),
					testutil.IgnoreDeploymentSpecDefaults(),
					testutil.IgnoreStatefulSetSpecDefaults(),
				),
				testutil.WithExtraResource(&multigresv1alpha1.Shard{}),
			)
			client := mgr.GetClient()

			shardReconciler := &shardcontroller.ShardReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			if err := shardReconciler.SetupWithManager(mgr, controller.Options{
				// Needed for the parallel test runs
				SkipNameValidation: ptr.To(true),
			}); err != nil {
				t.Fatalf("Failed to create controller, %v", err)
			}

			if err := client.Create(ctx, tc.shard); err != nil {
				t.Fatalf("Failed to create the initial item, %v", err)
			}

			if err := watcher.WaitForMatch(tc.wantResources...); err != nil {
				t.Errorf("Resources mismatch:\n%v", err)
			}
		})
	}
}

// Test helpers

// shardLabels returns standard labels for shard resources in tests
func shardLabels(t testing.TB, instanceName, component, cellName string) map[string]string {
	t.Helper()
	return map[string]string{
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/instance":   instanceName,
		"app.kubernetes.io/managed-by": "multigres-operator",
		"app.kubernetes.io/name":       "multigres",
		"app.kubernetes.io/part-of":    "multigres",
		"multigres.com/cell":           cellName,
		"multigres.com/database":       "testdb",
		"multigres.com/tablegroup":     "default",
	}
}

// shardOwnerRefs returns owner references for a Shard resource
func shardOwnerRefs(t testing.TB, shardName string) []metav1.OwnerReference {
	t.Helper()
	return []metav1.OwnerReference{{
		APIVersion:         "multigres.com/v1alpha1",
		Kind:               "Shard",
		Name:               shardName,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}}
}

// tcpPort creates a simple TCP container port
func tcpPort(t testing.TB, name string, port int32) corev1.ContainerPort {
	t.Helper()
	return corev1.ContainerPort{Name: name, ContainerPort: port, Protocol: corev1.ProtocolTCP}
}

// tcpServicePort creates a TCP service port with named target
func tcpServicePort(t testing.TB, name string, port int32) corev1.ServicePort {
	t.Helper()
	return corev1.ServicePort{Name: name, Port: port, TargetPort: intstr.FromString(name), Protocol: corev1.ProtocolTCP}
}

// multipoolerPorts returns the standard multipooler container ports
func multipoolerPorts(t testing.TB) []corev1.ContainerPort {
	t.Helper()
	return []corev1.ContainerPort{
		tcpPort(t, "http", 15200),
		tcpPort(t, "grpc", 15270),
		tcpPort(t, "postgres", 5432),
	}
}
