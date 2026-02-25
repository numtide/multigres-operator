//go:build e2e

package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// ---------------------------------------------------------------------------
// defaultE2EOpts
// ---------------------------------------------------------------------------

func TestDefaultE2EOpts(t *testing.T) {
	o := defaultE2EOpts()
	if !o.parallel {
		t.Error("parallel should be true by default")
	}
	if o.clusterWaitTime != 5*time.Minute {
		t.Errorf("clusterWaitTime = %v, want 5m", o.clusterWaitTime)
	}
	if o.kindConfigPath != "" {
		t.Errorf("kindConfigPath = %q, want empty", o.kindConfigPath)
	}
	if len(o.images) != 0 {
		t.Errorf("images = %v, want empty", o.images)
	}
	if len(o.setupFuncs) != 0 {
		t.Errorf("setupFuncs should be empty")
	}
	if len(o.finishFuncs) != 0 {
		t.Errorf("finishFuncs should be empty")
	}
}

// ---------------------------------------------------------------------------
// E2EOption functions
// ---------------------------------------------------------------------------

func TestWithSequential(t *testing.T) {
	o := defaultE2EOpts()
	WithSequential()(o)
	if o.parallel {
		t.Error("parallel should be false after WithSequential")
	}
}

func TestWithE2EKindConfig(t *testing.T) {
	o := defaultE2EOpts()
	WithE2EKindConfig("/path/to/config.yaml")(o)
	if o.kindConfigPath != "/path/to/config.yaml" {
		t.Errorf("kindConfigPath = %q, want %q", o.kindConfigPath, "/path/to/config.yaml")
	}
}

func TestWithImage(t *testing.T) {
	o := defaultE2EOpts()
	WithImage("img1:latest")(o)
	WithImage("img2:v2")(o)

	if len(o.images) != 2 {
		t.Fatalf("len(images) = %d, want 2", len(o.images))
	}
	if o.images[0] != "img1:latest" {
		t.Errorf("images[0] = %q, want %q", o.images[0], "img1:latest")
	}
	if o.images[1] != "img2:v2" {
		t.Errorf("images[1] = %q, want %q", o.images[1], "img2:v2")
	}
}

func TestWithSetup(t *testing.T) {
	o := defaultE2EOpts()
	WithSetup(func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		return ctx, nil
	})(o)
	if len(o.setupFuncs) != 1 {
		t.Errorf("len(setupFuncs) = %d, want 1", len(o.setupFuncs))
	}
}

func TestWithFinish(t *testing.T) {
	o := defaultE2EOpts()
	WithFinish(func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		return ctx, nil
	})(o)
	if len(o.finishFuncs) != 1 {
		t.Errorf("len(finishFuncs) = %d, want 1", len(o.finishFuncs))
	}
}

func TestWithClusterWait(t *testing.T) {
	o := defaultE2EOpts()
	WithClusterWait(10 * time.Minute)(o)
	if o.clusterWaitTime != 10*time.Minute {
		t.Errorf("clusterWaitTime = %v, want 10m", o.clusterWaitTime)
	}
}

// ---------------------------------------------------------------------------
// sanitizeE2EName
// ---------------------------------------------------------------------------

func TestSanitizeE2EName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"e2e-test-12345", "e2e-test-12345"},
		{"TestFoo/Bar", "testfoo-bar"},
		{"Test_Foo_Bar", "test-foo-bar"},
		{"Test Foo Bar", "test-foo-bar"},
		{"UPPER", "upper"},
		// Truncated to 50 chars
		{"a-very-long-name-that-exceeds-fifty-characters-and-should-be-truncated", "a-very-long-name-that-exceeds-fifty-characters-and"},
		// Trailing dashes stripped
		{"-leading-and-trailing-", "leading-and-trailing"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeE2EName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeE2EName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isPodReady
// ---------------------------------------------------------------------------

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "ready pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "not ready pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			want: false,
		},
		{
			name: "no conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{},
			},
			want: false,
		},
		{
			name: "other conditions only",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
						{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: false,
		},
		{
			name: "ready among multiple conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPodReady(tt.pod)
			if got != tt.want {
				t.Errorf("isPodReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// selectorFromDeployment
// ---------------------------------------------------------------------------

func TestSelectorFromDeployment(t *testing.T) {
	tests := []struct {
		name string
		dep  *appsv1.Deployment
		want string
	}{
		{
			name: "nil selector",
			dep:  &appsv1.Deployment{},
			want: "",
		},
		{
			name: "single label",
			dep: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			},
			want: "app=test",
		},
		{
			name: "multiple labels",
			dep: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":     "test",
							"version": "v1",
						},
					},
				},
			},
			want: "app=test,version=v1",
		},
		{
			name: "empty match labels",
			dep: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{},
					},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectorFromDeployment(tt.dep)
			if got != tt.want {
				t.Errorf("selectorFromDeployment() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// int64Ptr
// ---------------------------------------------------------------------------

func TestInt64Ptr(t *testing.T) {
	p := int64Ptr(42)
	if *p != 42 {
		t.Errorf("int64Ptr(42) = %d, want 42", *p)
	}
	p2 := int64Ptr(0)
	if *p2 != 0 {
		t.Errorf("int64Ptr(0) = %d, want 0", *p2)
	}
}

// ---------------------------------------------------------------------------
// DefaultOperatorPreset
// ---------------------------------------------------------------------------

func TestDefaultOperatorPreset_FromEnv(t *testing.T) {
	t.Setenv("OPERATOR_IMG", "my-registry/my-operator:v1.2.3")

	p := DefaultOperatorPreset()
	if p.Image != "my-registry/my-operator:v1.2.3" {
		t.Errorf("Image = %q, want %q", p.Image, "my-registry/my-operator:v1.2.3")
	}
	if p.Namespace != "multigres-operator" {
		t.Errorf("Namespace = %q, want %q", p.Namespace, "multigres-operator")
	}
	if p.DeploymentName != "multigres-operator-controller-manager" {
		t.Errorf("DeploymentName = %q, want %q", p.DeploymentName, "multigres-operator-controller-manager")
	}
	if p.ReadyTimeout != 3*time.Minute {
		t.Errorf("ReadyTimeout = %v, want 3m", p.ReadyTimeout)
	}
}

func TestDefaultOperatorPreset_Fallback(t *testing.T) {
	t.Setenv("OPERATOR_IMG", "")

	p := DefaultOperatorPreset()
	if p.Image != "ghcr.io/numtide/multigres-operator:dev" {
		t.Errorf("Image = %q, want fallback %q", p.Image, "ghcr.io/numtide/multigres-operator:dev")
	}
}

// ---------------------------------------------------------------------------
// MultigresImages
// ---------------------------------------------------------------------------

func TestMultigresImages(t *testing.T) {
	if len(MultigresImages) != 4 {
		t.Errorf("len(MultigresImages) = %d, want 4", len(MultigresImages))
	}
	// Verify all entries are non-empty
	for i, img := range MultigresImages {
		if img == "" {
			t.Errorf("MultigresImages[%d] is empty", i)
		}
	}
}

// ---------------------------------------------------------------------------
// TestCluster accessors
// ---------------------------------------------------------------------------

func TestTestClusterAccessors(t *testing.T) {
	tc := &TestCluster{
		clusterName: "test-cluster-123",
	}
	if tc.ClusterName() != "test-cluster-123" {
		t.Errorf("ClusterName() = %q, want %q", tc.ClusterName(), "test-cluster-123")
	}
}

// ---------------------------------------------------------------------------
// PortForwardResult.Stop
// ---------------------------------------------------------------------------

func TestPortForwardResultStop(t *testing.T) {
	stopCh := make(chan struct{})
	pf := &PortForwardResult{
		LocalPort: 12345,
		StopCh:    stopCh,
	}

	pf.Stop()

	// Verify channel is closed (receive returns immediately).
	select {
	case <-stopCh:
		// OK — closed
	default:
		t.Error("Stop() did not close StopCh")
	}
}

// ---------------------------------------------------------------------------
// maybeDestroyCluster (without a real cluster, we test the branching logic)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Option composition
// ---------------------------------------------------------------------------

func TestOptionsCompose(t *testing.T) {
	o := defaultE2EOpts()
	opts := []E2EOption{
		WithSequential(),
		WithImage("a:1"),
		WithImage("b:2"),
		WithE2EKindConfig("/kind.yaml"),
		WithClusterWait(7 * time.Minute),
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.parallel {
		t.Error("parallel should be false")
	}
	if len(o.images) != 2 {
		t.Fatalf("len(images) = %d, want 2", len(o.images))
	}
	if o.kindConfigPath != "/kind.yaml" {
		t.Errorf("kindConfigPath = %q", o.kindConfigPath)
	}
	if o.clusterWaitTime != 7*time.Minute {
		t.Errorf("clusterWaitTime = %v", o.clusterWaitTime)
	}
}

// ---------------------------------------------------------------------------
// TestCluster accessors — remaining ones
// ---------------------------------------------------------------------------

func TestTestCluster_NilAccessors(t *testing.T) {
	// Accessors should not panic even with nil fields — they just return nil.
	tc := &TestCluster{
		clusterName: "x",
	}
	if tc.Env() != nil {
		t.Error("Env() should be nil")
	}
	if tc.Config() != nil {
		t.Error("Config() should be nil")
	}
	if tc.RESTConfig() != nil {
		t.Error("RESTConfig() should be nil")
	}
	if tc.Clientset() != nil {
		t.Error("Clientset() should be nil")
	}
}

func TestTestCluster_WithConfig(t *testing.T) {
	cfg := envconf.New()
	testEnv := env.NewWithConfig(cfg)
	tc := &TestCluster{
		clusterName: "test",
		cfg:         cfg,
		env:         testEnv,
	}
	if tc.Config() != cfg {
		t.Error("Config() should return the assigned config")
	}
	if tc.KubeconfigFile() != cfg.KubeconfigFile() {
		t.Errorf("KubeconfigFile() = %q, want %q", tc.KubeconfigFile(), cfg.KubeconfigFile())
	}
	if tc.Env() == nil {
		t.Error("Env() should not be nil")
	}
	// KlientClient() requires a real kubeconfig — tested via e2e tests only.
}

// ---------------------------------------------------------------------------
// runFinish — exercises the loop
// ---------------------------------------------------------------------------

func TestRunFinish(t *testing.T) {
	called := false
	tc := &TestCluster{
		t:   t,
		cfg: envconf.New(),
	}
	tc.runFinish([]env.Func{
		func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
			called = true
			return ctx, nil
		},
	})
	if !called {
		t.Error("runFinish did not call the func")
	}
}

func TestRunFinish_Empty(t *testing.T) {
	tc := &TestCluster{
		t:   t,
		cfg: envconf.New(),
	}
	// Should not panic with empty slice.
	tc.runFinish(nil)
}

// ---------------------------------------------------------------------------
// clusterCleanupAction — pure decision logic
// ---------------------------------------------------------------------------

func TestClusterCleanupAction(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		failed bool
		want   cleanupDecision
	}{
		// Default (empty/never) — always destroy
		{"default passing", "", false, cleanupDestroy},
		{"default failed", "", true, cleanupDestroyWithHint},
		{"never passing", "never", false, cleanupDestroy},
		{"never failed", "never", true, cleanupDestroyWithHint},

		// on-failure — keep only on failure
		{"on-failure passing", "on-failure", false, cleanupDestroy},
		{"on-failure failed", "on-failure", true, cleanupKeep},

		// always — always keep
		{"always passing", "always", false, cleanupKeep},
		{"always failed", "always", true, cleanupKeep},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clusterCleanupAction(tt.policy, tt.failed)
			if got != tt.want {
				t.Errorf("clusterCleanupAction(%q, %v) = %d, want %d", tt.policy, tt.failed, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// maybeDestroyCluster — exercises the method with a real (fake) cluster
// ---------------------------------------------------------------------------

func TestMaybeDestroyCluster_Always(t *testing.T) {
	t.Setenv("E2E_KEEP_CLUSTERS", "always")

	tc := &TestCluster{
		t:           t,
		clusterName: "fake-cluster-always",
	}
	// Should not try to destroy — returns early with log.
	tc.maybeDestroyCluster()
}

func TestMaybeDestroyCluster_DefaultDestroy(t *testing.T) {
	t.Setenv("E2E_KEEP_CLUSTERS", "")

	tc := &TestCluster{
		t:           t,
		clusterName: "fake-cluster-default",
		cfg:         envconf.New(),
	}
	// Should try to destroy (will log but not crash since cluster doesn't exist).
	tc.maybeDestroyCluster()
}

func TestMaybeDestroyCluster_OnFailurePassingTest(t *testing.T) {
	t.Setenv("E2E_KEEP_CLUSTERS", "on-failure")

	tc := &TestCluster{
		t:           t,
		clusterName: "fake-cluster-onfailure",
		cfg:         envconf.New(),
	}
	// Test is passing, so it should destroy.
	tc.maybeDestroyCluster()
}

// Ensure unused imports are consumed.
var _ = os.Getenv
var _ = ptr.To[int32]
