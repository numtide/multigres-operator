package multigrescluster

import (
	"reflect"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
)

func TestBuildCell(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)

	cluster := &multigresv1alpha1.MultigresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
		Spec: multigresv1alpha1.MultigresClusterSpec{
			Images: multigresv1alpha1.ClusterImages{
				MultiGateway: "gateway:latest",
			},
		},
	}

	cellCfg := &multigresv1alpha1.CellConfig{
		Name: "zone-a",
		Zone: "us-east-1a",
	}

	gatewaySpec := &multigresv1alpha1.StatelessSpec{
		Replicas: ptr.To(int32(2)),
	}

	localTopoSpec := &multigresv1alpha1.LocalTopoServerSpec{} // details not critical for this test
	globalTopoRef := multigresv1alpha1.GlobalTopoServerRef{
		Address: "http://global-etcd:2379",
	}
	allCells := []multigresv1alpha1.CellName{"zone-a", "zone-b"}

	t.Run("Success", func(t *testing.T) {
		got, err := BuildCell(
			cluster,
			cellCfg,
			gatewaySpec,
			localTopoSpec,
			globalTopoRef,
			allCells,
			scheme,
		)
		if err != nil {
			t.Fatalf("BuildCell() error = %v", err)
		}

		// Calculate expected hash: md5("my-cluster", "zone-a") -> "6b6f7386"
		expectedName := names.JoinWithConstraints(names.DefaultConstraints, "my-cluster", "zone-a")
		if got.Name != expectedName {
			t.Errorf("Name = %v, want %v", got.Name, expectedName)
		}
		if got.Spec.Zone != "us-east-1a" {
			t.Errorf("Zone = %v, want %v", got.Spec.Zone, "us-east-1a")
		}
		if got.Spec.Images.MultiGateway != "gateway:latest" {
			t.Errorf("Gateway Image = %v, want %v", got.Spec.Images.MultiGateway, "gateway:latest")
		}
		if !reflect.DeepEqual(got.Spec.AllCells, allCells) {
			t.Errorf("AllCells = %v, want %v", got.Spec.AllCells, allCells)
		}

		// Verify OwnerReference
		if len(got.OwnerReferences) != 1 {
			t.Errorf("OwnerReferences count = %v, want 1", len(got.OwnerReferences))
		}
	})

	t.Run("ControllerRefError", func(t *testing.T) {
		emptyScheme := runtime.NewScheme()
		_, err := BuildCell(
			cluster,
			cellCfg,
			gatewaySpec,
			localTopoSpec,
			globalTopoRef,
			allCells,
			emptyScheme,
		)
		if err == nil {
			t.Error("Expected error due to missing scheme types, got nil")
		}
	})
}
