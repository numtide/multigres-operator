//go:build integration
// +build integration

package tablegroup_test

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/controller/tablegroup"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
	"github.com/numtide/multigres-operator/pkg/testutil"
)

func TestTableGroup_Lifecycle(t *testing.T) {
	t.Parallel()

	// Common test data
	globalTopo := multigresv1alpha1.GlobalTopoServerRef{
		Address:        "etcd-client:2379",
		RootPath:       "/multigres/global",
		Implementation: "etcd2",
	}

	// Setup Helper
	setup := func(t *testing.T) (client.Client, *testutil.ResourceWatcher) {
		t.Helper()

		// Create the Scheme and register types manually
		scheme := runtime.NewScheme()
		_ = multigresv1alpha1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		mgr := testutil.SetUpEnvtestManager(t, scheme,
			testutil.WithCRDPaths("../../../../config/crd/bases"),
		)

		// Start Controller
		if err := (&tablegroup.TableGroupReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr, controller.Options{SkipNameValidation: ptr.To(true)}); err != nil {
			t.Fatal(err)
		}

		watcher := testutil.NewResourceWatcher(t, t.Context(), mgr,
			testutil.WithCmpOpts(testutil.IgnoreMetaRuntimeFields()),
			testutil.WithExtraResource(&multigresv1alpha1.Shard{}),
			testutil.WithTimeout(10*time.Second),
		)
		return mgr.GetClient(), watcher
	}

	t.Run("Pruning (Orphan Deletion)", func(t *testing.T) {
		t.Parallel()
		k8sClient, watcher := setup(t)
		ctx := t.Context()

		tgName := "tg-prune"
		clusterName := "prune-cluster"
		tg := &multigresv1alpha1.TableGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tgName,
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": clusterName},
			},
			Spec: multigresv1alpha1.TableGroupSpec{
				DatabaseName: "db1", TableGroupName: "tg1",
				GlobalTopoServer: globalTopo,
				Shards: []multigresv1alpha1.ShardResolvedSpec{
					{
						Name:      "keep-me",
						MultiOrch: multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))}},
						Pools:     map[string]multigresv1alpha1.PoolSpec{},
					},
					{
						Name:      "delete-me",
						MultiOrch: multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))}},
						Pools:     map[string]multigresv1alpha1.PoolSpec{},
					},
				},
			},
		}

		// 1. Create Initial State
		if err := k8sClient.Create(ctx, tg); err != nil {
			t.Fatal(err)
		}

		// Wait for both shards
		shard1 := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      names.JoinWithConstraints(names.DefaultConstraints, clusterName, "db1", "tg1", "keep-me"),
				Namespace: "default",
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:     "db1",
				TableGroupName:   "tg1",
				ShardName:        "keep-me",
				GlobalTopoServer: globalTopo,
				MultiOrch:        multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))}},
				Pools:            map[string]multigresv1alpha1.PoolSpec{},
			},
		}
		shard2 := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      names.JoinWithConstraints(names.DefaultConstraints, clusterName, "db1", "tg1", "delete-me"),
				Namespace: "default",
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:     "db1",
				TableGroupName:   "tg1",
				ShardName:        "delete-me",
				GlobalTopoServer: globalTopo,
				MultiOrch:        multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))}},
				Pools:            map[string]multigresv1alpha1.PoolSpec{},
			},
		}

		// Using WaitForMatch with CompareSpecOnly to avoid needing full metadata/status matching
		watcher.SetCmpOpts(testutil.CompareSpecOnly()...)
		if err := watcher.WaitForMatch(shard1, shard2); err != nil {
			t.Fatal("Failed to create initial shards")
		}

		// 2. Update TG to remove "delete-me"
		// FIX: Use RetryOnConflict to handle background controller updates (e.g. status/finalizers)
		// causing ResourceVersion mismatches.
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Always fetch the latest version inside the retry loop
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tg), tg); err != nil {
				return err
			}
			tg.Spec.Shards = []multigresv1alpha1.ShardResolvedSpec{
				{
					Name:      "keep-me",
					MultiOrch: multigresv1alpha1.MultiOrchSpec{StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))}},
					Pools:     map[string]multigresv1alpha1.PoolSpec{},
				},
			}
			return k8sClient.Update(ctx, tg)
		}); err != nil {
			t.Fatal(err)
		}

		// 3. Verify Pruning using testutil
		// This uses the deletion watcher to confirm the resource is truly gone
		if err := watcher.WaitForDeletion(shard2); err != nil {
			t.Errorf("Shard 'delete-me' was not pruned: %v", err)
		}
	})

	t.Run("Enforcement (Revert Manual Changes)", func(t *testing.T) {
		t.Parallel()
		k8sClient, watcher := setup(t)
		ctx := t.Context()

		tgName := "tg-enforce"
		clusterName := "enforce-cluster"
		tg := &multigresv1alpha1.TableGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tgName,
				Namespace: "default",
				Labels:    map[string]string{"multigres.com/cluster": clusterName},
			},
			Spec: multigresv1alpha1.TableGroupSpec{
				DatabaseName:     "db1",
				TableGroupName:   "tg1",
				GlobalTopoServer: globalTopo,
				Shards: []multigresv1alpha1.ShardResolvedSpec{
					{
						Name: "s1",
						MultiOrch: multigresv1alpha1.MultiOrchSpec{
							StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
						},
						Pools: map[string]multigresv1alpha1.PoolSpec{},
					},
				},
			},
		}

		if err := k8sClient.Create(ctx, tg); err != nil {
			t.Fatal(err)
		}

		// Use SpecOnly comparison to simplify the object construction
		watcher.SetCmpOpts(testutil.CompareSpecOnly()...)

		// 1. Wait for Shard (Initial Good State)
		goodShard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      names.JoinWithConstraints(names.DefaultConstraints, clusterName, "db1", "tg1", "s1"),
				Namespace: "default",
			},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:     "db1",
				TableGroupName:   "tg1",
				ShardName:        "s1",
				GlobalTopoServer: globalTopo,
				MultiOrch: multigresv1alpha1.MultiOrchSpec{
					StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
				},
				Pools: map[string]multigresv1alpha1.PoolSpec{},
			},
		}
		if err := watcher.WaitForMatch(goodShard); err != nil {
			t.Fatal("Initial shard creation failed")
		}

		// 2. Tamper with Shard (Scale up manually)
		latestShard := &multigresv1alpha1.Shard{}
		// FIX: Use RetryOnConflict for tamper update as well, just in case
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(goodShard), latestShard); err != nil {
				return err
			}
			latestShard.Spec.MultiOrch.Replicas = ptr.To(int32(99)) // Tamper
			return k8sClient.Update(ctx, latestShard)
		}); err != nil {
			t.Fatal(err)
		}

		// 3. Verify Reversion (Back to Good State)
		// We removed the waiting for "Bad" state because the controller can be faster than the watcher.
		// If Update() succeeded, the tamper occurred. We now verify the controller enforces the desired state.
		if err := watcher.WaitForMatch(goodShard); err != nil {
			t.Errorf("Controller failed to revert manual shard change: %v", err)
		}
	})
}
