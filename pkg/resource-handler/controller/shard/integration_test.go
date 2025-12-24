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
					MultiOrch: multigresv1alpha1.MultiOrchSpec{
						Cells: []multigresv1alpha1.CellName{"us-west-1a", "us-west-1b"}, // 2 cells = 2 replicas
					},
					Pools: map[string]multigresv1alpha1.PoolSpec{
						"primary": {
							Cells:      []multigresv1alpha1.CellName{"us-west-1a"},
							Type:       "replica",
							ReplicasPerCell: ptr.To(int32(2)),
							Storage: multigresv1alpha1.StorageSpec{
								Size: "10Gi",
							},
						},
					},
				},
			},
			wantResources: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
						// TODO: Cell information is not mapped for MultiOrch
						// Labels:          shardLabels(t, "test-shard-multiorch", "multiorch", "us-west-1a"),
						Labels:          shardLabels(t, "test-shard-multiorch", "multiorch", "multigres-global-topo"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							// TODO: Cell information is not mapped for MultiOrch
							// MatchLabels:          shardLabels(t, "test-shard-multiorch", "multiorch", "us-west-1a"),
							MatchLabels: shardLabels(t, "test-shard-multiorch", "multiorch", "multigres-global-topo"),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								// TODO: Cell information is not mapped for MultiOrch
								// Labels:          shardLabels(t, "test-shard-multiorch", "multiorch", "us-west-1a"),
								Labels: shardLabels(t, "test-shard-multiorch", "multiorch", "multigres-global-topo"),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "multiorch",
										Image: "numtide/multigres-operator:latest",
										Args: []string{
											"--http-port", "15300",
											"--grpc-port", "15370",
											"--topo-implementation", "etcd2",
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
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shard-multiorch",
						Namespace: "default",
						// TODO: Cell information is not mapped for MultiOrch
						// Labels:          shardLabels(t, "test-shard-multiorch", "multiorch", "us-west-1a"),
						Labels:          shardLabels(t, "test-shard-multiorch", "multiorch", "multigres-global-topo"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
						Ports: []corev1.ServicePort{
							tcpServicePort(t, "http", 15300),
							tcpServicePort(t, "grpc", 15370),
						},
						// TODO: Cell information is not mapped for MultiOrch
						// Selector:          shardLabels(t, "test-shard-multiorch", "multiorch", "us-west-1a"),
						Selector: shardLabels(t, "test-shard-multiorch", "multiorch", "multigres-global-topo"),
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-shard-pool-primary",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-pool-primary", "shard-pool", "us-west-1a"),
						OwnerReferences: shardOwnerRefs(t, "test-shard"),
					},
					Spec: appsv1.StatefulSetSpec{
						ServiceName: "test-shard-pool-primary-headless",
						Replicas:    ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: shardLabels(t, "test-shard-pool-primary", "shard-pool", "us-west-1a"),
						},
						PodManagementPolicy: appsv1.ParallelPodManagement,
						UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
							Type: appsv1.RollingUpdateStatefulSetStrategyType,
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: shardLabels(t, "test-shard-pool-primary", "shard-pool", "us-west-1a"),
							},
							Spec: corev1.PodSpec{
								InitContainers: []corev1.Container{
									{
										Name:    "pgctld-init",
										Image:   "ghcr.io/multigres/pgctld:latest",
										Command: []string{"sh", "-c", "cp /pgctld /shared/pgctld && chmod +x /shared/pgctld"},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgctld-bin", MountPath: "/shared"},
										},
									},
									{
										Name:  "multipooler",
										Image: "ghcr.io/multigres/multipooler:latest",
										Args: []string{
											"--http-port", "15200",
											"--grpc-port", "15270",
											"--topo-implementation", "etcd2",
											"--cell", "us-west-1a",
											"--database", "testdb",
											"--table-group", "default",
											"--service-id", "test-shard-pool-primary",
											"--pgctld-addr", "localhost:15470",
											"--pg-port", "5432",
										},
										Ports:         multipoolerPorts(t),
										RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
									},
								},
								Containers: []corev1.Container{
									{
										Name:  "postgres",
										Image: "postgres:17",
										VolumeMounts: []corev1.VolumeMount{
											{Name: "pgdata", MountPath: "/var/lib/postgresql/data"},
											{Name: "pgctld-bin", MountPath: "/usr/local/bin/pgctld"},
										},
									},
								},
								Volumes: []corev1.Volume{
									{
										Name: "pgctld-bin",
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
						Name:            "test-shard-pool-primary-headless",
						Namespace:       "default",
						Labels:          shardLabels(t, "test-shard-pool-primary", "shard-pool", "us-west-1a"),
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
						Selector:                 shardLabels(t, "test-shard-pool-primary", "shard-pool", "us-west-1a"),
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
