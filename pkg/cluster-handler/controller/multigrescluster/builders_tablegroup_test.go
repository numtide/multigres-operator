package multigrescluster

import (
	"strings"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestBuildTableGroup(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}

	dbName := "my-db"
	globalTopoRef := multigresv1alpha1.GlobalTopoServerRef{}

	t.Run("Success", func(t *testing.T) {
		tgCfg := &multigresv1alpha1.TableGroupConfig{
			Name: "tg-1",
		}
		resolvedShards := []multigresv1alpha1.ShardResolvedSpec{
			{Name: "shard-0"},
		}

		got, err := BuildTableGroup(cluster, dbName, tgCfg, resolvedShards, globalTopoRef, scheme)
		if err != nil {
			t.Fatalf("BuildTableGroup() error = %v", err)
		}

		if got.Name != "my-cluster-my-db-tg-1" {
			t.Errorf("Name = %v, want %v", got.Name, "my-cluster-my-db-tg-1")
		}
		if got.Labels["multigres.com/database"] != "my-db" {
			t.Errorf("Label[database] = %v, want my-db", got.Labels["multigres.com/database"])
		}

		// Verify OwnerReference
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		}
	})

	t.Run("Name Too Long", func(t *testing.T) {
		longName := strings.Repeat("a", 250) // Very long name
		tgCfg := &multigresv1alpha1.TableGroupConfig{Name: longName}
		resolvedShards := []multigresv1alpha1.ShardResolvedSpec{}

		got, err := BuildTableGroup(cluster, dbName, tgCfg, resolvedShards, globalTopoRef, scheme)
		if err != nil {
			t.Errorf("BuildTableGroup() error = %v, want nil", err)
		}
		// Cluster Name (10) + Hash (8) + Hyphen (1) = 19 chars
		// expected prefix: "my-cluster-"
		if !strings.HasPrefix(got.Name, "my-cluster-") {
			t.Errorf("Expected hashed name starting with 'my-cluster-', got %s", got.Name)
		}
		if len(got.Name) > 63 {
			t.Errorf("Expected name length <= 63, got %d", len(got.Name))
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		tgCfg := &multigresv1alpha1.TableGroupConfig{Name: "tg1"}
		resolvedShards := []multigresv1alpha1.ShardResolvedSpec{}
		emptyScheme := runtime.NewScheme()

		_, err := BuildTableGroup(
			cluster,
			dbName,
			tgCfg,
			resolvedShards,
			globalTopoRef,
			emptyScheme,
		)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}
