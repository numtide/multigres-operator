//go:build e2e

package scaling_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/numtide/multigres-operator/test/e2e/framework"
)

var cluster *framework.Cluster

func TestMain(m *testing.M) {
	var err error
	cluster, err = framework.EnsureSharedCluster()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
