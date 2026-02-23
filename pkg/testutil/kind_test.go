//go:build e2e

package testutil

import (
	"os"
	"testing"
)

func TestDefaultKindConfig_ClusterNameFromEnv(t *testing.T) {
	t.Setenv("KIND_CLUSTER", "my-custom-cluster")

	cfg := defaultKindConfig()
	if cfg.clusterName != "my-custom-cluster" {
		t.Errorf("clusterName = %q, want %q", cfg.clusterName, "my-custom-cluster")
	}
}

func TestDefaultKindConfig_ClusterNameFallback(t *testing.T) {
	t.Setenv("KIND_CLUSTER", "")

	cfg := defaultKindConfig()
	if cfg.clusterName != defaultKindCluster {
		t.Errorf("clusterName = %q, want %q", cfg.clusterName, defaultKindCluster)
	}
}

func TestDefaultKindConfig_ClusterNameUnset(t *testing.T) {
	// Unset the env var entirely (Setenv("", "") sets it to empty string)
	os.Unsetenv("KIND_CLUSTER")
	t.Setenv("KIND_CLUSTER", "")

	cfg := defaultKindConfig()
	if cfg.clusterName != defaultKindCluster {
		t.Errorf("clusterName = %q, want %q", cfg.clusterName, defaultKindCluster)
	}
}

func TestDefaultKindConfig_KubectlFromEnv(t *testing.T) {
	t.Setenv("KUBECTL", "/usr/local/bin/kubectl")

	cfg := defaultKindConfig()
	if cfg.kubectl != "/usr/local/bin/kubectl" {
		t.Errorf("kubectl = %q, want %q", cfg.kubectl, "/usr/local/bin/kubectl")
	}
}

func TestDefaultKindConfig_KubectlFallback(t *testing.T) {
	t.Setenv("KUBECTL", "")

	cfg := defaultKindConfig()
	if cfg.kubectl != defaultKubectl {
		t.Errorf("kubectl = %q, want %q", cfg.kubectl, defaultKubectl)
	}
}

func TestWithKindCluster(t *testing.T) {
	cfg := defaultKindConfig()
	WithKindCluster("override-cluster")(cfg)

	if cfg.clusterName != "override-cluster" {
		t.Errorf("clusterName = %q, want %q", cfg.clusterName, "override-cluster")
	}
}

func TestWithKindKubectl(t *testing.T) {
	cfg := defaultKindConfig()
	WithKindKubectl("/opt/bin/kubectl")(cfg)

	if cfg.kubectl != "/opt/bin/kubectl" {
		t.Errorf("kubectl = %q, want %q", cfg.kubectl, "/opt/bin/kubectl")
	}
}

func TestWithKindCRDPaths(t *testing.T) {
	cfg := defaultKindConfig()
	WithKindCRDPaths("/path/to/crds", "/another/path")(cfg)

	if len(cfg.crdPaths) != 2 {
		t.Fatalf("len(crdPaths) = %d, want 2", len(cfg.crdPaths))
	}
	if cfg.crdPaths[0] != "/path/to/crds" {
		t.Errorf("crdPaths[0] = %q, want %q", cfg.crdPaths[0], "/path/to/crds")
	}
	if cfg.crdPaths[1] != "/another/path" {
		t.Errorf("crdPaths[1] = %q, want %q", cfg.crdPaths[1], "/another/path")
	}
}

func TestRandomSuffix(t *testing.T) {
	s1 := randomSuffix()
	s2 := randomSuffix()

	if len(s1) != 8 {
		t.Errorf("len(randomSuffix()) = %d, want 8", len(s1))
	}
	if s1 == s2 {
		t.Errorf("randomSuffix() returned same value twice: %q", s1)
	}
}
