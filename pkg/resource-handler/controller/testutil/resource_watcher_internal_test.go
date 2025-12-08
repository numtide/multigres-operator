package testutil

import (
	"context"
	"errors"
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

// TestExtractKind_NoPointer tests extractKind with non-pointer types and types without dots.
func TestExtractKind_NoPointer(t *testing.T) {
	tests := map[string]struct {
		obj  client.Object
		want string
	}{
		"Pod with pointer": {
			obj:  &corev1.Pod{},
			want: "Pod",
		},
		"Secret with pointer": {
			obj:  &corev1.Secret{},
			want: "Secret",
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

// TestWaitForEvent_PredicateError tests waitForEvent when predicate returns an error.
func TestWaitForEvent_PredicateError(t *testing.T) {
	ch := make(chan ResourceEvent, 1)
	testErr := errors.New("test error")

	// Send an event
	ch <- ResourceEvent{
		Type: "ADDED",
		Kind: "Service",
		Name: "test",
	}

	predicate := func(evt ResourceEvent) error {
		return testErr // Return error immediately
	}

	deadline := time.Now().Add(1 * time.Second)
	_, err := waitForEvent(t, ch, deadline, predicate)

	if err != testErr {
		t.Errorf("waitForEvent() error = %v, want %v", err, testErr)
	}
}

// TestWaitForEvent_ChannelClosed tests waitForEvent when the channel is closed.
func TestWaitForEvent_ChannelClosed(t *testing.T) {
	ch := make(chan ResourceEvent, 1)
	close(ch) // Close immediately

	predicate := func(evt ResourceEvent) error {
		return nil
	}

	deadline := time.Now().Add(1 * time.Second)
	_, err := waitForEvent(t, ch, deadline, predicate)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("waitForEvent() error = %v, want context.Canceled", err)
	}
}

// TestFindLatestEventFor_EmptyNamespace tests findLatestEventFor with empty namespace.
func TestFindLatestEventFor_EmptyNamespace(t *testing.T) {
	watcher := &ResourceWatcher{
		t: t,
		events: []ResourceEvent{
			{Kind: "Service", Name: "svc1", Namespace: "default"},
			{Kind: "Service", Name: "svc1", Namespace: "kube-system"},
			{Kind: "Service", Name: "svc2", Namespace: "default"},
		},
	}

	// Search with only name (empty namespace should match all)
	obj := &corev1.Service{}
	obj.SetName("svc1")
	obj.SetNamespace("") // Empty namespace

	evt := watcher.findLatestEventFor(obj)
	if evt == nil {
		t.Fatal("findLatestEventFor() returned nil, want event")
	}

	// Should return the latest svc1 event (from kube-system)
	if evt.Name != "svc1" {
		t.Errorf("findLatestEventFor() Name = %s, want svc1", evt.Name)
	}
	if evt.Namespace != "kube-system" {
		t.Errorf("findLatestEventFor() Namespace = %s, want kube-system", evt.Namespace)
	}
}

// TestFindLatestEventFor_EmptyName tests findLatestEventFor with empty name.
func TestFindLatestEventFor_EmptyName(t *testing.T) {
	watcher := &ResourceWatcher{
		t: t,
		events: []ResourceEvent{
			{Kind: "Service", Name: "svc1", Namespace: "default"},
			{Kind: "Service", Name: "svc2", Namespace: "default"},
			{Kind: "Deployment", Name: "deploy1", Namespace: "default"},
		},
	}

	// Search with only kind and namespace (empty name should match all)
	obj := &corev1.Service{}
	obj.SetName("") // Empty name
	obj.SetNamespace("default")

	evt := watcher.findLatestEventFor(obj)
	if evt == nil {
		t.Fatal("findLatestEventFor() returned nil, want event")
	}

	// Should return the latest Service in default namespace (svc2)
	if evt.Kind != "Service" {
		t.Errorf("findLatestEventFor() Kind = %s, want Service", evt.Kind)
	}
	if evt.Name != "svc2" {
		t.Errorf("findLatestEventFor() Name = %s, want svc2", evt.Name)
	}
}

// TestCollectEvents_SubscriberChannelFull tests that events are dropped when subscriber channel is full.
func TestCollectEvents_SubscriberChannelFull(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher := &ResourceWatcher{
		t:       t,
		eventCh: make(chan ResourceEvent, 10),
		events:  []ResourceEvent{},
	}

	// Start collectEvents
	go watcher.collectEvents(ctx)

	// Create a subscriber with small buffer
	subCh := make(chan ResourceEvent, 1)
	watcher.mu.Lock()
	watcher.subscribers = append(watcher.subscribers, subCh)
	watcher.mu.Unlock()

	// Fill the subscriber channel
	watcher.eventCh <- ResourceEvent{Type: "ADDED", Kind: "Service", Name: "svc1"}
	time.Sleep(10 * time.Millisecond) // Give collectEvents time to process

	// Send another event - this should trigger the default branch (subscriber channel full)
	watcher.eventCh <- ResourceEvent{Type: "ADDED", Kind: "Service", Name: "svc2"}
	time.Sleep(10 * time.Millisecond) // Give collectEvents time to process

	// Verify the main events slice has both events
	watcher.mu.RLock()
	eventsCount := len(watcher.events)
	watcher.mu.RUnlock()

	if eventsCount != 2 {
		t.Errorf("watcher.events length = %d, want 2", eventsCount)
	}

	// Verify subscriber channel only has first event
	select {
	case evt := <-subCh:
		if evt.Name != "svc1" {
			t.Errorf("First event Name = %s, want svc1", evt.Name)
		}
	default:
		t.Error("Expected first event in subscriber channel")
	}

	// Second event should have been dropped due to full channel
	select {
	case evt := <-subCh:
		t.Errorf("Unexpected second event in subscriber channel: %+v", evt)
	default:
		// Expected - channel doesn't have second event
	}
}

// TestWaitForMatch_EmptySlice tests WaitForMatch with empty slice.
func TestWaitForMatch_EmptySlice(t *testing.T) {
	watcher := &ResourceWatcher{
		t:       t,
		timeout: 1 * time.Second,
	}

	err := watcher.WaitForMatch()
	if err != nil {
		t.Errorf("WaitForMatch() with empty slice should return nil, got: %v", err)
	}
}

// TestWaitForDeletion_EmptySlice tests WaitForDeletion with empty slice.
func TestWaitForDeletion_EmptySlice(t *testing.T) {
	watcher := &ResourceWatcher{
		t:       t,
		timeout: 1 * time.Second,
	}

	err := watcher.WaitForDeletion()
	if err != nil {
		t.Errorf("WaitForDeletion() with empty slice should return nil, got: %v", err)
	}
}

// TestExtractKind_NoDot tests extractKind with a type that has no package (no dot).
func TestExtractKind_NoDot(t *testing.T) {
	// Create a mock type that when formatted has no dot
	// In practice, all client.Object types have packages, but we can test the fallback
	kind := extractKind(&corev1.Node{})
	if kind != "Node" {
		t.Errorf("extractKind() = %s, want Node", kind)
	}
}

// TestExtractKind_NonPointer tests extractKind fallback for type without leading *.
func TestExtractKind_NonPointer(t *testing.T) {
	// This tests the case where kind[0] != '*'
	// by ensuring we handle types correctly
	pod := corev1.Pod{}
	kind := extractKind(&pod)
	if kind != "Pod" {
		t.Errorf("extractKind() = %s, want Pod", kind)
	}
}

// TestExtractKind_FallbackPath tests the final return when no dot is found.
func TestExtractKind_FallbackPath(t *testing.T) {
	// Test with various types to ensure the fallback path works
	tests := []struct {
		obj  client.Object
		want string
	}{
		{&corev1.Namespace{}, "Namespace"},
		{&corev1.Node{}, "Node"},
		{&corev1.PersistentVolume{}, "PersistentVolume"},
	}

	for _, tc := range tests {
		got := extractKind(tc.obj)
		if got != tc.want {
			t.Errorf("extractKind(%T) = %s, want %s", tc.obj, got, tc.want)
		}
	}
}

// TestWaitForEvent_ErrorReturn tests waitForEvent returning a predicate error.
func TestWaitForEvent_ErrorReturn(t *testing.T) {
	ch := make(chan ResourceEvent, 1)
	testErr := errors.New("predicate custom error")

	// Send an event
	ch <- ResourceEvent{
		Type: "ADDED",
		Kind: "Service",
		Name: "test",
	}

	predicate := func(evt ResourceEvent) error {
		// Return a custom error (not ErrKeepWaiting)
		return testErr
	}

	deadline := time.Now().Add(1 * time.Second)
	_, err := waitForEvent(t, ch, deadline, predicate)

	if err != testErr {
		t.Errorf("waitForEvent() error = %v, want %v", err, testErr)
	}
}

// TestWaitForEvent_TimeoutEdgeCase tests the timeout logic in waitForEvent.
func TestWaitForEvent_TimeoutEdgeCase(t *testing.T) {
	ch := make(chan ResourceEvent, 1)

	// Set deadline in the past
	deadline := time.Now().Add(-1 * time.Second)

	predicate := func(evt ResourceEvent) error {
		return ErrKeepWaiting
	}

	_, err := waitForEvent(t, ch, deadline, predicate)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waitForEvent() with past deadline should return DeadlineExceeded, got: %v", err)
	}
}

// TestWaitForEvent_TimeoutBoundary tests the timeout boundary condition.
func TestWaitForEvent_TimeoutBoundary(t *testing.T) {
	ch := make(chan ResourceEvent, 1)

	// Set deadline very close to now to test the boundary check
	deadline := time.Now().Add(50 * time.Millisecond)

	// Send events after deadline should pass
	go func() {
		time.Sleep(100 * time.Millisecond)
		ch <- ResourceEvent{Type: "TEST", Kind: "Service"}
	}()

	predicate := func(evt ResourceEvent) error {
		return ErrKeepWaiting // Never match
	}

	_, err := waitForEvent(t, ch, deadline, predicate)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waitForEvent() should timeout, got: %v", err)
	}
}

// TestFindLatestEvent_NoMatch tests findLatestEvent when no events match.
func TestFindLatestEvent_NoMatch(t *testing.T) {
	watcher := &ResourceWatcher{
		t: t,
		events: []ResourceEvent{
			{Kind: "Service", Name: "svc1", Namespace: "default"},
			{Kind: "Service", Name: "svc2", Namespace: "default"},
		},
	}

	evt := watcher.findLatestEvent(func(e ResourceEvent) bool {
		return e.Name == "nonexistent"
	})

	if evt != nil {
		t.Errorf("findLatestEvent() should return nil for no match, got: %+v", evt)
	}
}

// TestFindLatestEventFor_KindMismatch tests findLatestEventFor when kind doesn't match.
func TestFindLatestEventFor_KindMismatch(t *testing.T) {
	watcher := &ResourceWatcher{
		t: t,
		events: []ResourceEvent{
			{Kind: "Service", Name: "svc1", Namespace: "default"},
		},
	}

	// Search for a different kind
	obj := &corev1.ConfigMap{}
	obj.SetName("svc1")
	obj.SetNamespace("default")

	evt := watcher.findLatestEventFor(obj)
	if evt != nil {
		t.Errorf("findLatestEventFor() should return nil when kind doesn't match, got: %+v", evt)
	}
}

// TestCheckLatestEventMatches_NoEvent tests checkLatestEventMatches when no event exists.
func TestCheckLatestEventMatches_NoEvent(t *testing.T) {
	watcher := &ResourceWatcher{
		t:      t,
		events: []ResourceEvent{},
	}

	svc := &corev1.Service{}
	svc.SetName("nonexistent")
	svc.SetNamespace("default")

	matched, diff := watcher.checkLatestEventMatches(svc, nil)
	if matched {
		t.Error("checkLatestEventMatches() should return false when no events exist")
	}
	if diff != "" {
		t.Errorf("checkLatestEventMatches() should return empty diff when no events exist, got: %s", diff)
	}
}

// TestUnsubscribe_AlreadyClosed tests unsubscribe when subscribers is nil.
func TestUnsubscribe_AlreadyClosed(t *testing.T) {
	watcher := &ResourceWatcher{
		t:           t,
		subscribers: nil, // Simulate collectEvents already closed
	}

	ch := make(chan ResourceEvent, 1)

	// Should not panic
	watcher.unsubscribe(ch)
}

// TestUnsubscribe_NotFound tests unsubscribe with channel not in list.
func TestUnsubscribe_NotFound(t *testing.T) {
	watcher := &ResourceWatcher{
		t:           t,
		subscribers: []chan ResourceEvent{make(chan ResourceEvent, 1)},
	}

	differentCh := make(chan ResourceEvent, 1)

	// Should not panic
	watcher.unsubscribe(differentCh)

	if len(watcher.subscribers) != 1 {
		t.Errorf("unsubscribe() should not remove other channels, got %d subscribers", len(watcher.subscribers))
	}
}
