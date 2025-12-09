package testutil

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
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
		timeout: 20 * time.Second,
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
		timeout: 5 * time.Second,
	}

	opt := WithTimeout(customTimeout)
	opt(watcher)

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
	option(watcher)

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
	option(watcher)

	if len(watcher.extraResources) != 2 {
		t.Errorf(
			"WithExtraResource() set extraResources length = %d, want 2",
			len(watcher.extraResources),
		)
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

	opt1(watcher)
	opt2(watcher)

	// Both are added to extraResources slice
	if len(watcher.extraResources) != 2 {
		t.Errorf(
			"WithExtraResource() called twice set extraResources length = %d, want 2",
			len(watcher.extraResources),
		)
	}

	// Note: watchResource() deduplicates by checking watchedKinds map,
	// so the second ConfigMap won't create duplicate event handlers
}
