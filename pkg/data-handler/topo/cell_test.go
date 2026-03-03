package topo_test

import (
	"context"
	"testing"

	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/topo"
)

func newTestCell(name string) *multigresv1alpha1.Cell {
	return &multigresv1alpha1.Cell{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: multigresv1alpha1.CellSpec{
			Name: multigresv1alpha1.CellName(name),
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{
				Address:  "localhost:2379",
				RootPath: "/test",
			},
		},
	}
}

func TestRegisterCell(t *testing.T) {
	t.Parallel()

	t.Run("creates new cell in topology", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		recorder := record.NewFakeRecorder(10)
		cell := newTestCell("cell1")

		if err := topo.RegisterCell(context.Background(), store, recorder, cell); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := store.GetCell(context.Background(), "cell1")
		if err != nil {
			t.Fatalf("cell not found in topo after registration: %v", err)
		}
		if got.Name != "cell1" {
			t.Errorf("expected cell name cell1, got %s", got.Name)
		}
	})

	t.Run("idempotent when cell already exists", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		recorder := record.NewFakeRecorder(10)
		cell := newTestCell("cell1")

		if err := topo.RegisterCell(context.Background(), store, recorder, cell); err != nil {
			t.Fatalf("first registration failed: %v", err)
		}
		if err := topo.RegisterCell(context.Background(), store, recorder, cell); err != nil {
			t.Fatalf("second registration should succeed (idempotent), got: %v", err)
		}
	})
}

func TestUnregisterCell(t *testing.T) {
	t.Parallel()

	t.Run("removes existing cell", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		recorder := record.NewFakeRecorder(10)
		cell := newTestCell("cell1")
		ctx := context.Background()

		if err := topo.RegisterCell(ctx, store, recorder, cell); err != nil {
			t.Fatalf("registration failed: %v", err)
		}
		if err := topo.UnregisterCell(ctx, store, recorder, cell); err != nil {
			t.Fatalf("unregistration failed: %v", err)
		}

		_, err := store.GetCell(ctx, "cell1")
		if err == nil {
			t.Error("expected cell to be gone from topo after unregistration")
		}
	})

	t.Run("idempotent when cell does not exist", func(t *testing.T) {
		t.Parallel()
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		defer func() { _ = store.Close() }()

		recorder := record.NewFakeRecorder(10)
		cell := newTestCell("nonexistent")

		if err := topo.UnregisterCell(context.Background(), store, recorder, cell); err != nil {
			t.Fatalf("unregistering nonexistent cell should succeed (idempotent), got: %v", err)
		}
	})
}
