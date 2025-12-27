package resolver

import (
	"context"
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// setupFixtures helper returns a fresh set of test objects.
func setupFixtures(t testing.TB) (
	*multigresv1alpha1.CoreTemplate,
	*multigresv1alpha1.CellTemplate,
	*multigresv1alpha1.ShardTemplate,
	string,
) {
	t.Helper()
	namespace := "default"

	coreTpl := &multigresv1alpha1.CoreTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.CoreTemplateSpec{
			GlobalTopoServer: &multigresv1alpha1.TopoServerSpec{
				Etcd: &multigresv1alpha1.EtcdSpec{Image: "core-default"},
			},
		},
	}

	cellTpl := &multigresv1alpha1.CellTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.CellTemplateSpec{
			MultiGateway: &multigresv1alpha1.StatelessSpec{
				Replicas: ptr.To(int32(1)),
			},
		},
	}

	shardTpl := &multigresv1alpha1.ShardTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
		Spec: multigresv1alpha1.ShardTemplateSpec{
			MultiOrch: &multigresv1alpha1.MultiOrchSpec{
				StatelessSpec: multigresv1alpha1.StatelessSpec{Replicas: ptr.To(int32(1))},
			},
		},
	}

	return coreTpl, cellTpl, shardTpl, namespace
}

func TestNewResolver(t *testing.T) {
	t.Parallel()

	c := fake.NewClientBuilder().Build()
	defaults := multigresv1alpha1.TemplateDefaults{CoreTemplate: "foo"}
	r := NewResolver(c, "ns", defaults)

	if got, want := r.Client, c; got != want {
		t.Errorf("Client mismatch: got %v, want %v", got, want)
	}
	if got, want := r.Namespace, "ns"; got != want {
		t.Errorf("Namespace mismatch: got %q, want %q", got, want)
	}
	if got, want := r.TemplateDefaults.CoreTemplate, "foo"; got != want {
		t.Errorf("Defaults mismatch: got %q, want %q", got, want)
	}
}

// mockClient is a partial implementation of client.Client to force errors.
type mockClient struct {
	client.Client
	failGet bool
	err     error
}

func (m *mockClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if m.failGet {
		return m.err
	}
	return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
}

func parseQty(s string) resource.Quantity {
	return resource.MustParse(s)
}
