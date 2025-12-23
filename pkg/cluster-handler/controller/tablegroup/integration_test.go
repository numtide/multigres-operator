//go:build integration
// +build integration

package tablegroup_test

import (
	"path/filepath"
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
	"github.com/numtide/multigres-operator/pkg/cluster-handler/controller/tablegroup"
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

	if err := (&tablegroup.TableGroupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, controller.Options{
		SkipNameValidation: ptr.To(true),
	}); err != nil {
		t.Fatalf("Failed to create controller, %v", err)
	}
}

func TestTableGroupReconciliation(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := map[string]struct {
		tableGroup    *multigresv1alpha1.TableGroup
		wantResources []client.Object
	}{
		"simple tablegroup creates shards": {
			tableGroup: &multigresv1alpha1.TableGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tg-test-simple",
					Namespace: "default",
					Labels: map[string]string{
						"multigres.com/cluster":    "test-cluster",
						"multigres.com/database":   "db1",
						"multigres.com/tablegroup": "tg1",
					},
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
						Address:        "etcd:2379",
						RootPath:       "/multigres/global",
						Implementation: "etcd2",
					},
					Shards: []multigresv1alpha1.ShardResolvedSpec{
						{
							Name: "s1",
							MultiOrch: multigresv1alpha1.MultiOrchSpec{
								StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
								Cells:         []multigresv1alpha1.CellName{"zone-a"},
							},
							Pools: map[string]multigresv1alpha1.PoolSpec{
								"primary": {
									Type:            "readWrite",
									ReplicasPerCell: ptr.To(int32(1)),
									Cells:           []multigresv1alpha1.CellName{"zone-a"},
								},
							},
						},
						{
							Name: "s2",
							MultiOrch: multigresv1alpha1.MultiOrchSpec{
								StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
								Cells:         []multigresv1alpha1.CellName{"zone-b"},
							},
							Pools: map[string]multigresv1alpha1.PoolSpec{
								"primary": {
									Type:            "readWrite",
									ReplicasPerCell: ptr.To(int32(1)),
									Cells:           []multigresv1alpha1.CellName{"zone-b"},
								},
							},
						},
					},
				},
			},
			wantResources: []client.Object{
				// Shard 1
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tg-test-simple-s1",
						Namespace: "default",
						Labels: map[string]string{
							"multigres.com/cluster":    "test-cluster",
							"multigres.com/database":   "db1",
							"multigres.com/tablegroup": "tg1",
							"multigres.com/shard":      "s1",
						},
						OwnerReferences: tgOwnerRefs(t, "tg-test-simple"),
					},
					Spec: multigresv1alpha1.ShardSpec{
						ShardName:      "s1",
						DatabaseName:   "db1",
						TableGroupName: "tg1",
						Images: multigresv1alpha1.ShardImages{
							MultiOrch:   "orch:latest",
							MultiPooler: "pooler:latest",
							Postgres:    "postgres:15",
						},
						GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
							Address:        "etcd:2379",
							RootPath:       "/multigres/global",
							Implementation: "etcd2",
						},
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
							Cells:         []multigresv1alpha1.CellName{"zone-a"},
						},
						Pools: map[string]multigresv1alpha1.PoolSpec{
							"primary": {
								Type:            "readWrite",
								ReplicasPerCell: ptr.To(int32(1)),
								Cells:           []multigresv1alpha1.CellName{"zone-a"},
							},
						},
					},
				},
				// Shard 2
				&multigresv1alpha1.Shard{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tg-test-simple-s2",
						Namespace: "default",
						Labels: map[string]string{
							"multigres.com/cluster":    "test-cluster",
							"multigres.com/database":   "db1",
							"multigres.com/tablegroup": "tg1",
							"multigres.com/shard":      "s2",
						},
						OwnerReferences: tgOwnerRefs(t, "tg-test-simple"),
					},
					Spec: multigresv1alpha1.ShardSpec{
						ShardName:      "s2",
						DatabaseName:   "db1",
						TableGroupName: "tg1",
						Images: multigresv1alpha1.ShardImages{
							MultiOrch:   "orch:latest",
							MultiPooler: "pooler:latest",
							Postgres:    "postgres:15",
						},
						GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
							Address:        "etcd:2379",
							RootPath:       "/multigres/global",
							Implementation: "etcd2",
						},
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
							Cells:         []multigresv1alpha1.CellName{"zone-b"},
						},
						Pools: map[string]multigresv1alpha1.PoolSpec{
							"primary": {
								Type:            "readWrite",
								ReplicasPerCell: ptr.To(int32(1)),
								Cells:           []multigresv1alpha1.CellName{"zone-b"},
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

			// 2. Setup Watcher
			watcher := testutil.NewResourceWatcher(t, ctx, mgr,
				testutil.WithCmpOpts(
					testutil.IgnoreMetaRuntimeFields(),
				),
				testutil.WithExtraResource(
					&multigresv1alpha1.TableGroup{},
					&multigresv1alpha1.Shard{},
				),
				testutil.WithTimeout(10*time.Second),
			)
			k8sClient := mgr.GetClient()

			// 3. Setup and Start Controller
			reconciler := &tablegroup.TableGroupReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}

			if err := reconciler.SetupWithManager(mgr, controller.Options{
				SkipNameValidation: ptr.To(true),
			}); err != nil {
				t.Fatalf("Failed to create controller, %v", err)
			}

			// 4. Create the Input
			if err := k8sClient.Create(ctx, tc.tableGroup); err != nil {
				t.Fatalf("Failed to create the initial tablegroup, %v", err)
			}

			// 5. Assert Logic
			if err := watcher.WaitForMatch(tc.wantResources...); err != nil {
				t.Errorf("Resources mismatch:\n%v", err)
			}
		})
	}
}

// Helpers

func tgOwnerRefs(t testing.TB, tgName string) []metav1.OwnerReference {
	t.Helper()
	return []metav1.OwnerReference{{
		APIVersion:         "multigres.com/v1alpha1",
		Kind:               "TableGroup",
		Name:               tgName,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}}
}
