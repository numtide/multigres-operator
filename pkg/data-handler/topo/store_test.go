package topo_test

import (
	"errors"
	"testing"

	_ "github.com/multigres/multigres/go/common/topoclient/etcdtopo"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres-operator/pkg/data-handler/topo"
)

func TestNewStoreFromShard(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		shard   *multigresv1alpha1.Shard
		wantErr bool
	}{
		"creates store for etcd": {
			shard: &multigresv1alpha1.Shard{
				Spec: multigresv1alpha1.ShardSpec{
					GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
						Address:  "localhost:2379",
						RootPath: "/test",
					},
				},
			},
			wantErr: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := topo.NewStoreFromShard(tc.shard)
			if (err != nil) != tc.wantErr {
				t.Errorf("NewStoreFromShard() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if !tc.wantErr && store != nil {
				_ = store.Close()
			}
		})
	}
}

func TestNewStoreFromShard_InvalidImplementation(t *testing.T) {
	shard := &multigresv1alpha1.Shard{
		Spec: multigresv1alpha1.ShardSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:        "localhost:2379",
				RootPath:       "/test",
				Implementation: "invalid-implementation-that-does-not-exist",
			},
		},
	}

	store, err := topo.NewStoreFromShard(shard)
	if err == nil {
		if store != nil {
			_ = store.Close()
		}
		t.Error("NewStoreFromShard() should error with invalid implementation")
	}
}

func TestIsTopoUnavailable(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want bool
	}{
		"nil error":           {err: nil, want: false},
		"UNAVAILABLE error":   {err: errors.New("Code: UNAVAILABLE"), want: true},
		"no connection error": {err: errors.New("no connection available"), want: true},
		"unrelated error":     {err: errors.New("permission denied"), want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := topo.IsTopoUnavailable(tc.err); got != tc.want {
				t.Errorf("IsTopoUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestNewStoreFromCell(t *testing.T) {
	t.Parallel()

	cell := &multigresv1alpha1.Cell{
		Spec: multigresv1alpha1.CellSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:  "localhost:2379",
				RootPath: "/test",
			},
		},
	}

	store, err := topo.NewStoreFromCell(cell)
	if err != nil {
		t.Fatalf("NewStoreFromCell() unexpected error: %v", err)
	}
	if store != nil {
		_ = store.Close()
	}
}

func TestNewStoreFromCell_InvalidImplementation(t *testing.T) {
	cell := &multigresv1alpha1.Cell{
		Spec: multigresv1alpha1.CellSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:        "localhost:2379",
				RootPath:       "/test",
				Implementation: "invalid-implementation-that-does-not-exist",
			},
		},
	}

	store, err := topo.NewStoreFromCell(cell)
	if err == nil {
		if store != nil {
			_ = store.Close()
		}
		t.Error("NewStoreFromCell() should error with invalid implementation")
	}
}

func TestNewStoreFromRef(t *testing.T) {
	t.Parallel()

	ref := multigresv1alpha1.GlobalTopoServerRef{
		Address:  "localhost:2379",
		RootPath: "/test",
	}

	store, err := topo.NewStoreFromRef(ref)
	if err != nil {
		t.Fatalf("NewStoreFromRef() unexpected error: %v", err)
	}
	if store != nil {
		_ = store.Close()
	}
}

func TestNewStoreFromRef_InvalidImplementation(t *testing.T) {
	ref := multigresv1alpha1.GlobalTopoServerRef{
		Address:        "localhost:2379",
		RootPath:       "/test",
		Implementation: "invalid-implementation-that-does-not-exist",
	}

	store, err := topo.NewStoreFromRef(ref)
	if err == nil {
		if store != nil {
			_ = store.Close()
		}
		t.Error("NewStoreFromRef() should error with invalid implementation")
	}
}
