//go:build integration
// +build integration

package multigrescluster_test

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/controller/multigrescluster"
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

	if err := (&multigrescluster.MultigresClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, controller.Options{
		SkipNameValidation: ptr.To(true),
	}); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

func TestMultigresClusterReconciliation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	const (
		clusterName = "test-integration-cluster"
		namespace   = "default"
	)

	tests := map[string]struct {
		cluster       *multigresv1alpha1.MultigresCluster
		wantResources []client.Object
	}{
		"full cluster integration": {
			cluster: &multigresv1alpha1.MultigresCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: multigresv1alpha1.MultigresClusterSpec{
					Images: multigresv1alpha1.ClusterImages{
						MultiGateway: "gateway:latest",
						MultiOrch:    "orch:latest",
						MultiPooler:  "pooler:latest",
						MultiAdmin:   "admin:latest",
						Postgres:     "postgres:15",
					},
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{Image: "etcd:latest"},
					},
					MultiAdmin: multigresv1alpha1.MultiAdminConfig{
						Spec: &multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
					},
					Cells: []multigresv1alpha1.CellConfig{
						{Name: "zone-a", Zone: "us-east-1a", Spec: &multigresv1alpha1.CellInlineSpec{
							MultiGateway: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
						}},
					},
					Databases: []multigresv1alpha1.DatabaseConfig{
						{
							Name: "db1",
							TableGroups: []multigresv1alpha1.TableGroupConfig{
								{Name: "tg1", Shards: []multigresv1alpha1.ShardConfig{{
									Name: "s1",
									Spec: &multigresv1alpha1.ShardInlineSpec{
										MultiOrch: multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))}},
										Pools: map[string]multigresv1alpha1.PoolSpec{
											"primary": {
												ReplicasPerCell: ptr.To(int32(1)),
												Type:            "readWrite",
												// We must explicitly assign cells so they propagate to MultiOrch
												Cells: []multigresv1alpha1.CellName{"zone-a"},
											},
										},
									},
								}}},
							},
						},
					},
				},
			},
			wantResources: []client.Object{
				// Note: We verify child resources first. Parent finalizer is checked manually below.

				// 1. Global TopoServer
				&multigresv1alpha1.TopoServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:            clusterName + "-global-topo",
						Namespace:       namespace,
						Labels:          clusterLabels(t, clusterName, "", ""),
						OwnerReferences: clusterOwnerRefs(t, clusterName),
					},
					Spec: multigresv1alpha1.TopoServerSpec{
						Etcd: &multigresv1alpha1.EtcdSpec{
							Image:    "etcd:latest",
							Replicas: ptr.To(int32(3)), // Default from logic
						},
					},
				},
				// 2. MultiAdmin Deployment
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            clusterName + "-multiadmin",
						Namespace:       namespace,
						Labels:          clusterLabels(t, clusterName, "multiadmin", ""),
						OwnerReferences: clusterOwnerRefs(t, clusterName),
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: ptr.To(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: clusterLabels(t, clusterName, "multiadmin", ""),
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: clusterLabels(t, clusterName, "multiadmin", ""),
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "multiadmin",
										Image: "admin:latest",
									},
								},
							},
						},
					},
				},
				// 3. Cell
				&multigresv1alpha1.Cell{
					ObjectMeta: metav1.ObjectMeta{
						Name:            clusterName + "-zone-a",
						Namespace:       namespace,
						Labels:          clusterLabels(t, clusterName, "", "zone-a"),
						OwnerReferences: clusterOwnerRefs(t, clusterName),
					},
					Spec: multigresv1alpha1.CellSpec{
						Name:              "zone-a",
						Zone:              "us-east-1a",
						MultiGatewayImage: "gateway:latest",
						MultiGateway: multigresv1alpha1.StatelessSpec{
							Replicas: ptr.To(int32(1)),
						},
						AllCells: []multigresv1alpha1.CellName{"zone-a"},
						GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
							Address:        clusterName + "-global-topo-client." + namespace + ".svc:2379",
							RootPath:       "/multigres/global",
							Implementation: "etcd2",
						},
						TopologyReconciliation: multigresv1alpha1.TopologyReconciliation{
							RegisterCell: true,
							PrunePoolers: true,
						},
					},
				},
				// 4. TableGroup
				&multigresv1alpha1.TableGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName + "-db1-tg1",
						Namespace: namespace,
						Labels: map[string]string{
							"multigres.com/cluster":    clusterName,
							"multigres.com/database":   "db1",
							"multigres.com/tablegroup": "tg1",
						},
						OwnerReferences: clusterOwnerRefs(t, clusterName),
					},
					Spec: multigresv1alpha1.TableGroupSpec{
						DatabaseName:   "db1",
						TableGroupName: "tg1",
						Images: multigresv1alpha1.ShardImages{
							MultiOrch:   "orch:latest",
							MultiPooler: "pooler:latest",
							Postgres:    "postgres:15",
						},
						GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
							Address:        clusterName + "-global-topo-client." + namespace + ".svc:2379",
							RootPath:       "/multigres/global",
							Implementation: "etcd2",
						},
						Shards: []multigresv1alpha1.ShardResolvedSpec{
							{
								Name: "s1",
								MultiOrch: multigresv1alpha1.MultiOrchSpec{
									// Controller logic defaults cells if empty
									Cells:         []multigresv1alpha1.CellName{"zone-a"},
									StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
								},
								Pools: map[string]multigresv1alpha1.PoolSpec{
									"primary": {
										ReplicasPerCell: ptr.To(int32(1)),
										Type:            "readWrite",
										Cells:           []multigresv1alpha1.CellName{"zone-a"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			// 1. Setup Envtest and Manager
			mgr := testutil.SetUpEnvtestManager(t, scheme,
				testutil.WithCRDPaths(
					filepath.Join("../../../../", "config", "crd", "bases"),
				),
			)

			// 2. Setup Watcher for all expected resources
			// We explicitly watch the child resources we expect to be created.
			watcher := testutil.NewResourceWatcher(t, ctx, mgr,
				testutil.WithCmpOpts(
					testutil.IgnoreMetaRuntimeFields(),
					testutil.IgnoreServiceRuntimeFields(),
					testutil.IgnoreDeploymentRuntimeFields(),
					testutil.IgnorePodSpecDefaults(),
					testutil.IgnoreDeploymentSpecDefaults(),
				),
				testutil.WithExtraResource(
					&multigresv1alpha1.MultigresCluster{},
					&multigresv1alpha1.TopoServer{},
					&multigresv1alpha1.Cell{},
					&multigresv1alpha1.TableGroup{},
				),
				// Extend timeout as this is a "root" controller triggering other things
				testutil.WithTimeout(10*time.Second),
			)
			k8sClient := mgr.GetClient()

			// 3. Setup and Start Controller
			reconciler := &multigrescluster.MultigresClusterReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}

			// Pass SkipNameValidation via options to avoid controller name collisions in parallel tests
			if err := reconciler.SetupWithManager(mgr, controller.Options{
				SkipNameValidation: ptr.To(true),
			}); err != nil {
				t.Fatalf("Failed to create controller, %v", err)
			}

			// 4. Create the Input
			if err := k8sClient.Create(ctx, tc.cluster); err != nil {
				t.Fatalf("Failed to create the initial cluster, %v", err)
			}

			// 5. Assert Logic: Wait for Children
			// This ensures the controller has run and reconciled at least once successfully
			if err := watcher.WaitForMatch(tc.wantResources...); err != nil {
				t.Errorf("Resources mismatch:\n%v", err)
			}

			// 6. Verify Parent Finalizer (Manual Check)
			// We check this manually to avoid fighting with status/spec diffs in the watcher
			fetchedCluster := &multigresv1alpha1.MultigresCluster{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tc.cluster), fetchedCluster); err != nil {
				t.Fatalf("Failed to get cluster: %v", err)
			}

			if !slices.Contains(fetchedCluster.Finalizers, "multigres.com/finalizer") {
				t.Errorf("Expected finalizer 'multigres.com/finalizer' to be present, got %v", fetchedCluster.Finalizers)
			}
		})
	}
}

// Helpers

func clusterLabels(t testing.TB, clusterName, app, cell string) map[string]string {
	t.Helper()
	l := map[string]string{
		"multigres.com/cluster": clusterName,
	}
	if app != "" {
		l["app"] = app
	}
	if cell != "" {
		l["multigres.com/cell"] = cell
	}
	return l
}

func clusterOwnerRefs(t testing.TB, clusterName string) []metav1.OwnerReference {
	t.Helper()
	return []metav1.OwnerReference{{
		APIVersion:         "multigres.com/v1alpha1",
		Kind:               "MultigresCluster",
		Name:               clusterName,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}}
}
