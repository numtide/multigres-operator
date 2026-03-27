package shard

import (
	"context"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
)

// newMemoryTopoFactory returns a CreateTopoStore factory backed by an
// in-memory topology server. Safe for concurrent use by parallel subtests.
func newMemoryTopoFactory() func(*multigresv1alpha1.Shard) (topoclient.Store, error) {
	return func(_ *multigresv1alpha1.Shard) (topoclient.Store, error) {
		_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
		store := topoclient.NewWithFactory(
			factory, "", []string{""}, topoclient.NewDefaultTopoConfig(),
		)
		return store, nil
	}
}
