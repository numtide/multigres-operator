package tablegroup

import (
	"reflect"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/numtide/multigres-operator/pkg/util/name"
)

func TestBuildShard(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	tg := &multigresv1alpha1.TableGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-tg",
			Namespace: "default",
			UID:       "tg-uid",
			Labels: map[string]string{
				"multigres.com/cluster":  "my-cluster",
				"multigres.com/database": "my-db",
			},
		},
		Spec: multigresv1alpha1.TableGroupSpec{
			DatabaseName:   "my-db",
			TableGroupName: "my-tg",
		},
	}

	shardSpec := &multigresv1alpha1.ShardResolvedSpec{
		Name: "shard-0",
		Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
			"default": {Type: "transaction"},
		},
	}

	t.Run("Success", func(t *testing.T) {
		got, err := BuildShard(tg, shardSpec, scheme)
		if err != nil {
			t.Fatalf("BuildShard() error = %v", err)
		}

		// Calculate expected hash: md5("my-cluster", "my-db", "my-tg", "shard-0") -> "a068d59f"
		expectedName := name.JoinWithConstraints(
			name.DefaultConstraints,
			"my-cluster",
			"my-db",
			"my-tg",
			"shard-0",
		)
		if got.Name != expectedName {
			t.Errorf("Name = %v, want %v", got.Name, expectedName)
		}
		if got.Namespace != "default" {
			t.Errorf("Namespace = %v, want %v", got.Namespace, "default")
		}
		if got.Labels["multigres.com/cluster"] != "my-cluster" {
			t.Errorf(
				"Labels[cluster] = %v, want %v",
				got.Labels["multigres.com/cluster"],
				"my-cluster",
			)
		}
		// Verify OwnerReference pointing to TableGroup
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		} else {
			if got.OwnerReferences[0].Name != "my-tg" {
				t.Errorf("OwnerReference Name = %v, want %v", got.OwnerReferences[0].Name, "my-tg")
			}
			if got.OwnerReferences[0].UID != "tg-uid" {
				t.Errorf("OwnerReference UID = %v, want %v", got.OwnerReferences[0].UID, "tg-uid")
			}
		}

		// Verify Spec fields are copied
		if got.Spec.ShardName != "shard-0" {
			t.Errorf("Spec.ShardName = %v, want %v", got.Spec.ShardName, "shard-0")
		}
		if got.Spec.DatabaseName != "my-db" {
			t.Errorf("Spec.DatabaseName = %v, want %v", got.Spec.DatabaseName, "my-db")
		}
		if !reflect.DeepEqual(got.Spec.Pools, shardSpec.Pools) {
			t.Errorf("Spec.Pools = %v, want %v", got.Spec.Pools, shardSpec.Pools)
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		// Intentionally missing scheme registrations to force SetControllerReference failure
		_, err := BuildShard(tg, shardSpec, emptyScheme)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}
