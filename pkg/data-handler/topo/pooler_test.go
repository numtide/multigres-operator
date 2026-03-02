package topo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/topo"
)

func TestFindPrimaryPooler(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when no primary exists", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName:   "db",
				TableGroupName: "tg",
				ShardName:      "0",
			},
		}

		primary, err := topo.FindPrimaryPooler(
			context.Background(),
			store,
			shard,
			[]string{"cell1"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if primary != nil {
			t.Error("expected nil primary when none registered")
		}
	})

	t.Run("returns error for non-unavailable topo errors", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell2")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName: "db", TableGroupName: "tg", ShardName: "0",
			},
		}

		_, err := topo.FindPrimaryPooler(
			context.Background(), store, shard, []string{"nonexistent-cell"},
		)
		if err == nil {
			t.Error("expected error for non-unavailable topo error")
		}
	})

	t.Run("returns primary from second cell", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1", "cell2")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:       &clustermetadata.ID{Cell: "cell1", Name: "replica-pod"},
			Hostname: "replica-pod", Type: clustermetadata.PoolerType_REPLICA,
			Database: "db", TableGroup: "tg", Shard: "0",
		}, false)
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:       &clustermetadata.ID{Cell: "cell2", Name: "primary-pod"},
			Hostname: "primary-pod", Type: clustermetadata.PoolerType_PRIMARY,
			Database: "db", TableGroup: "tg", Shard: "0",
		}, false)

		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName: "db", TableGroupName: "tg", ShardName: "0",
			},
		}

		primary, err := topo.FindPrimaryPooler(ctx, store, shard, []string{"cell1", "cell2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if primary == nil {
			t.Fatal("expected primary to be found")
		}
		if primary.Id.Name != "primary-pod" {
			t.Errorf("expected primary-pod, got %s", primary.Id.Name)
		}
	})
}

// multiCellStore is a mock store that returns different results per cell.
type multiCellStore struct {
	topoclient.Store
	cells      map[string][]*topoclient.MultiPoolerInfo
	errorCells map[string]error
}

func (s *multiCellStore) GetMultiPoolersByCell(
	ctx context.Context, cell string, opts *topoclient.GetMultiPoolersByCellOptions,
) ([]*topoclient.MultiPoolerInfo, error) {
	if err, ok := s.errorCells[cell]; ok {
		return nil, err
	}
	return s.cells[cell], nil
}

func (s *multiCellStore) Close() error { return nil }

func TestFindPrimaryPooler_TopoUnavailableSkip(t *testing.T) {
	t.Parallel()

	store := &multiCellStore{
		cells: map[string][]*topoclient.MultiPoolerInfo{
			"cell2": {{
				MultiPooler: &clustermetadata.MultiPooler{
					Id:       &clustermetadata.ID{Cell: "cell2", Name: "primary-pod"},
					Hostname: "primary-pod", Type: clustermetadata.PoolerType_PRIMARY,
				},
			}},
		},
		errorCells: map[string]error{
			"cell1": errors.New("Code: UNAVAILABLE"),
		},
	}

	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName: "db", TableGroupName: "tg", ShardName: "0",
		},
	}

	primary, err := topo.FindPrimaryPooler(
		context.Background(), store, shard, []string{"cell1", "cell2"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary == nil {
		t.Fatal("expected primary from cell2 after skipping unavailable cell1")
	}
	if primary.Id.Name != "primary-pod" {
		t.Errorf("expected primary-pod, got %s", primary.Id.Name)
	}
}

func TestForceUnregisterPod(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for empty cell label", func(t *testing.T) {
		t.Parallel()
		shard := &multigresv1alpha1.Shard{}
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		err := topo.ForceUnregisterPod(context.Background(), store, shard, "pod", "")
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("skips unregistration when no matching pooler found", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:       &clustermetadata.ID{Cell: "cell1", Name: "other-pod"},
			Hostname: "other-pod", Type: clustermetadata.PoolerType_REPLICA,
			Database: "db", TableGroup: "tg", Shard: "0",
		}, false)

		shard := &multigresv1alpha1.Shard{
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName: "db", TableGroupName: "tg", ShardName: "0",
			},
		}

		err := topo.ForceUnregisterPod(ctx, store, shard, "nonexistent-pod", "cell1")
		if err != nil {
			t.Errorf("expected nil error for missing pooler, got %v", err)
		}

		poolers, _ := store.GetMultiPoolersByCell(ctx, "cell1", nil)
		if len(poolers) != 1 {
			t.Errorf("expected 1 pooler remaining, got %d", len(poolers))
		}
	})
}

// errorGetPoolersStore returns an error for GetMultiPoolersByCell.
type errorGetPoolersStore struct {
	topoclient.Store
}

func (s *errorGetPoolersStore) GetMultiPoolersByCell(
	ctx context.Context, cell string, opts *topoclient.GetMultiPoolersByCellOptions,
) ([]*topoclient.MultiPoolerInfo, error) {
	return nil, errors.New("topo error")
}

func (s *errorGetPoolersStore) Close() error { return nil }

func TestForceUnregisterPod_GetPoolersError(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName: "db", TableGroupName: "tg", ShardName: "0",
		},
	}

	store := &errorGetPoolersStore{}
	err := topo.ForceUnregisterPod(context.Background(), store, shard, "test-pod", "cell1")
	if err == nil {
		t.Error("expected error when GetMultiPoolersByCell fails")
	}
}

func TestCollectCells(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"primary": {Cells: []multigresv1alpha1.CellName{"zone-a", "zone-b"}},
				"replica": {Cells: []multigresv1alpha1.CellName{"zone-b", "zone-c"}},
			},
		},
	}

	cells := topo.CollectCells(shard)
	if len(cells) != 3 {
		t.Errorf("expected 3 unique cells, got %d: %v", len(cells), cells)
	}

	cellSet := make(map[string]bool)
	for _, c := range cells {
		cellSet[c] = true
	}
	for _, want := range []string{"zone-a", "zone-b", "zone-c"} {
		if !cellSet[want] {
			t.Errorf("expected cell %q in result", want)
		}
	}
}

func TestGetPoolerStatus(t *testing.T) {
	t.Parallel()

	t.Run("returns roles for all pooler types", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:       &clustermetadata.ID{Cell: "cell1", Name: "primary"},
			Hostname: "primary", Type: clustermetadata.PoolerType_PRIMARY,
			Database: "db", TableGroup: "tg", Shard: "0",
		}, false)
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:       &clustermetadata.ID{Cell: "cell1", Name: "replica"},
			Hostname: "replica", Type: clustermetadata.PoolerType_REPLICA,
			Database: "db", TableGroup: "tg", Shard: "0",
		}, false)
		_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
			Id:       &clustermetadata.ID{Cell: "cell1", Name: "drained"},
			Hostname: "drained", Type: clustermetadata.PoolerType_DRAINED,
			Database: "db", TableGroup: "tg", Shard: "0",
		}, false)

		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName: "db", TableGroupName: "tg", ShardName: "0",
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
				},
			},
		}

		result := topo.GetPoolerStatus(ctx, store, shard)
		if !result.QuerySuccess {
			t.Error("expected QuerySuccess=true")
		}
		if result.Roles["primary"] != "PRIMARY" {
			t.Errorf("expected PRIMARY, got %s", result.Roles["primary"])
		}
		if result.Roles["replica"] != "REPLICA" {
			t.Errorf("expected REPLICA, got %s", result.Roles["replica"])
		}
		if result.Roles["drained"] != "DRAINED" {
			t.Errorf("expected DRAINED, got %s", result.Roles["drained"])
		}
	})

	t.Run("sets QuerySuccess false on error", func(t *testing.T) {
		t.Parallel()
		store := &errorGetPoolersStore{}

		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: multigresv1alpha1.ShardSpec{
				DatabaseName: "db", TableGroupName: "tg", ShardName: "0",
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
				},
			},
		}

		result := topo.GetPoolerStatus(context.Background(), store, shard)
		if result.QuerySuccess {
			t.Error("expected QuerySuccess=false when store errors")
		}
	})
}
