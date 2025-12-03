package testutil

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestSetTimeout verifies SetTimeout updates the timeout field.
func TestSetTimeout(t *testing.T) {
	watcher := &ResourceWatcher{
		t:       t,
		timeout: 5 * time.Second,
	}

	newTimeout := 20 * time.Second
	watcher.SetTimeout(newTimeout)

	if watcher.timeout != newTimeout {
		t.Errorf("SetTimeout() timeout = %v, want %v", watcher.timeout, newTimeout)
	}
}

// TestResetTimeout verifies ResetTimeout restores default (5 seconds).
func TestResetTimeout(t *testing.T) {
	watcher := &ResourceWatcher{
		t:       t,
		timeout:20 * time.Second,
	}

	watcher.ResetTimeout()

	expectedDefault := 5 * time.Second
	if watcher.timeout != expectedDefault {
		t.Errorf("ResetTimeout() timeout = %v, want %v (default)", watcher.timeout, expectedDefault)
	}
}

// TestSetCmpOpts verifies SetCmpOpts updates the cmpOpts field.
func TestSetCmpOpts(t *testing.T) {
	watcher := &ResourceWatcher{
		t:       t,
		cmpOpts: nil,
	}

	newOpts := []cmp.Option{IgnoreMetaRuntimeFields(), IgnoreStatus()}
	watcher.SetCmpOpts(newOpts...)

	if len(watcher.cmpOpts) != 2 {
		t.Errorf("SetCmpOpts() cmpOpts length = %d, want 2", len(watcher.cmpOpts))
	}
}

// TestResetCmpOpts verifies ResetCmpOpts clears the cmpOpts field.
func TestResetCmpOpts(t *testing.T) {
	watcher := &ResourceWatcher{
		t:       t,
		cmpOpts: []cmp.Option{IgnoreStatus()},
	}

	watcher.ResetCmpOpts()

	if watcher.cmpOpts != nil {
		t.Errorf("ResetCmpOpts() cmpOpts = %v, want nil", watcher.cmpOpts)
	}
}

// TestWithTimeout verifies WithTimeout option.
func TestWithTimeout(t *testing.T) {
	customTimeout := 30 * time.Second
	watcher := &ResourceWatcher{
		t:       t,
		timeout:5 * time.Second,
	}

	opt := WithTimeout(customTimeout)
	if err := opt(watcher); err != nil {
		t.Fatalf("WithTimeout() error = %v", err)
	}

	if watcher.timeout != customTimeout {
		t.Errorf("WithTimeout() set timeout = %v, want %v", watcher.timeout, customTimeout)
	}
}

// TestWithCmpOpts verifies WithCmpOpts option.
func TestWithCmpOpts(t *testing.T) {
	opts := []cmp.Option{IgnoreMetaRuntimeFields(), IgnoreStatus()}
	watcher := &ResourceWatcher{
		t:       t,
		cmpOpts: nil,
	}

	option := WithCmpOpts(opts...)
	if err := option(watcher); err != nil {
		t.Fatalf("WithCmpOpts() error = %v", err)
	}

	if len(watcher.cmpOpts) != 2 {
		t.Errorf("WithCmpOpts() set cmpOpts length = %d, want 2", len(watcher.cmpOpts))
	}
}

// TestWithExtraResource verifies WithExtraResource option.
func TestWithExtraResource(t *testing.T) {
	watcher := &ResourceWatcher{
		extraResources: []client.Object{},
	}

	configMap := &corev1.ConfigMap{}
	secret := &corev1.Secret{}

	option := WithExtraResource(configMap, secret)
	if err := option(watcher); err != nil {
		t.Fatalf("WithExtraResource() error = %v", err)
	}

	if len(watcher.extraResources) != 2 {
		t.Errorf("WithExtraResource() set extraResources length = %d, want 2", len(watcher.extraResources))
	}
}

// TestWithExtraResource_Duplicates verifies duplicate kinds are handled.
func TestWithExtraResource_Duplicates(t *testing.T) {
	watcher := &ResourceWatcher{
		extraResources: []client.Object{},
	}

	cm1 := &corev1.ConfigMap{}
	cm2 := &corev1.ConfigMap{} // Same kind

	// Call twice with same kind
	opt1 := WithExtraResource(cm1)
	opt2 := WithExtraResource(cm2)

	if err := opt1(watcher); err != nil {
		t.Fatalf("First WithExtraResource() error = %v", err)
	}
	if err := opt2(watcher); err != nil {
		t.Fatalf("Second WithExtraResource() error = %v", err)
	}

	// Both are added to extraResources slice
	if len(watcher.extraResources) != 2 {
		t.Errorf("WithExtraResource() called twice set extraResources length = %d, want 2", len(watcher.extraResources))
	}

	// Note: watchResource() deduplicates by checking watchedKinds map,
	// so the second ConfigMap won't create duplicate event handlers
}

// TestExtractKind tests kind extraction from various object types.
func TestExtractKind(t *testing.T) {
	tests := map[string]struct {
		obj  client.Object
		want string
	}{
		"Service with pointer": {
			obj:  &corev1.Service{},
			want: "Service",
		},
		"ConfigMap with pointer": {
			obj:  &corev1.ConfigMap{},
			want: "ConfigMap",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := extractKind(tc.obj)
			if got != tc.want {
				t.Errorf("extractKind() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestSendEvent_ChannelFull tests the default branch when event channel is full.
func TestSendEvent_ChannelFull(t *testing.T) {
	watcher := &ResourceWatcher{
		t:       t,
		eventCh: make(chan ResourceEvent, 1), // Buffer size 1
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}

	// Fill the channel
	watcher.sendEvent("ADDED", "Service", svc)

	// This should hit the default branch (channel full, event dropped)
	watcher.sendEvent("UPDATED", "Service", svc)

	// Verify first event is in channel
	evt := <-watcher.eventCh
	if evt.Name != "test" {
		t.Errorf("Event in channel has Name = %s, want test", evt.Name)
	}
	if evt.Type != "ADDED" {
		t.Errorf("Event in channel has Type = %s, want ADDED", evt.Type)
	}

	// Second event was dropped (channel was full)
	select {
	case <-watcher.eventCh:
		t.Error("Channel should be empty (second event was dropped)")
	default:
		// Expected - channel is empty because second event was dropped
	}
}
