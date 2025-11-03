package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ResourceEvent represents a Kubernetes resource event.
type ResourceEvent struct {
	Type      string // "ADDED", "UPDATED", "DELETED"
	Kind      string // "Service", "StatefulSet", "Deployment", etc.
	Name      string
	Namespace string
	Object    client.Object // The actual object (type-assert to specific type)
	Time      time.Time
}

// ResourceWatcher collects events from multiple resource types.
type ResourceWatcher struct {
	mu             sync.RWMutex
	events         []ResourceEvent
	eventCh        chan ResourceEvent
	subscribers    []chan ResourceEvent // Fan-out channels for WaitForMatch
	t              testing.TB
	extraResources []client.Object
}

type Option func(rw *ResourceWatcher) error

// WithExtraResource adds a watch for an additional resource type. The object
// should be a pointer reference to the struct such as a custom resource.
//
// If you need to watch multiple resources, you can provide the list of
// resources.
func WithExtraResource(objs ...client.Object) Option {
	return func(rw *ResourceWatcher) error {
		rw.extraResources = append(rw.extraResources, objs...)
		return nil
	}
}

// NewResourceWatcher creates a new ResourceWatcher and automatically watches
// Service, StatefulSet, and Deployment resources.
func NewResourceWatcher(t testing.TB, ctx context.Context, mgr manager.Manager, opts ...Option) *ResourceWatcher {
	t.Helper()

	watcher := &ResourceWatcher{
		events:  []ResourceEvent{},
		eventCh: make(chan ResourceEvent, 1000),
		t:       t,
	}
	for _, o := range opts {
		if err := o(watcher); err != nil {
			t.Fatalf("Failed to set up watcher: %v", err)
		}
	}

	// Start background collector
	go watcher.collectEvents(ctx)

	// Automatically watch standard resources
	if err := watcher.watchResource(ctx, mgr, &corev1.Service{}); err != nil {
		t.Fatalf("Failed to watch Service: %v", err)
	}
	if err := watcher.watchResource(ctx, mgr, &appsv1.StatefulSet{}); err != nil {
		t.Fatalf("Failed to watch StatefulSet: %v", err)
	}
	if err := watcher.watchResource(ctx, mgr, &appsv1.Deployment{}); err != nil {
		t.Fatalf("Failed to watch Deployment: %v", err)
	}

	// Watch extra resources provided
	for _, res := range watcher.extraResources {
		if err := watcher.watchResource(ctx, mgr, res); err != nil {
			t.Fatalf("Failed to watch custom resource %v: %v", res, err)
		}
	}

	return watcher
}

// EventChan returns the channel for receiving events directly.
// Useful for custom event processing logic.
func (rw *ResourceWatcher) EventChan() <-chan ResourceEvent {
	return rw.eventCh
}

// Events returns a snapshot of all collected events at the current time.
func (rw *ResourceWatcher) Events() []ResourceEvent {
	rw.t.Helper()

	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return append([]ResourceEvent{}, rw.events...)
}

// ForKind returns events for a specific resource kind.
func (rw *ResourceWatcher) ForKind(kind string) []ResourceEvent {
	rw.t.Helper()

	rw.mu.RLock()
	defer rw.mu.RUnlock()

	var filtered []ResourceEvent
	for _, evt := range rw.events {
		if evt.Kind == kind {
			filtered = append(filtered, evt)
		}
	}
	return filtered
}

// ForName returns events for a specific resource name (across all kinds).
func (rw *ResourceWatcher) ForName(name string) []ResourceEvent {
	rw.t.Helper()

	rw.mu.RLock()
	defer rw.mu.RUnlock()

	var filtered []ResourceEvent
	for _, evt := range rw.events {
		if evt.Name == name {
			filtered = append(filtered, evt)
		}
	}
	return filtered
}

// Count returns the total number of events collected.
func (rw *ResourceWatcher) Count() int {
	rw.t.Helper()

	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return len(rw.events)
}

// collectEvents runs in the background collecting events and fanning out to subscribers.
func (rw *ResourceWatcher) collectEvents(ctx context.Context) {
	for {
		select {
		case evt := <-rw.eventCh:
			rw.mu.Lock()
			// Store in main events slice
			rw.events = append(rw.events, evt)

			// Fan out to all subscribers
			for _, subCh := range rw.subscribers {
				select {
				case subCh <- evt:
					// Event sent to subscriber
				default:
					// Subscriber channel full, skip (they'll timeout)
					rw.t.Logf("Warning: subscriber channel full, dropping event")
				}
			}
			rw.mu.Unlock()
		case <-ctx.Done():
			// Close all subscriber channels
			rw.mu.Lock()
			for _, subCh := range rw.subscribers {
				close(subCh)
			}
			rw.subscribers = nil
			rw.mu.Unlock()
			return
		}
	}
}

// WaitForMatch waits for a resource of the specified kind to match the expected object
// using go-cmp comparison. Returns nil when matched, error on timeout.
//
// First checks existing events for early return, then subscribes to new events.
//
// Example:
//   expectedSts := &appsv1.StatefulSet{
//       Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3))},
//   }
//   err := watcher.WaitForMatch("StatefulSet", expectedSts, 10*time.Second, testutil.CompareSpecOnly()...)
//   if err != nil {
//       t.Errorf("StatefulSet never reached expected state: %v", err)
//   }
func (rw *ResourceWatcher) WaitForMatch(kind string, expected client.Object, timeout time.Duration, cmpOpts ...cmp.Option) error {
	rw.t.Helper()

	// Step 1: Check existing events first (early return optimization)
	rw.mu.RLock()
	for _, evt := range rw.events {
		if evt.Kind != kind {
			continue
		}
		if diff := cmp.Diff(expected, evt.Object, cmpOpts...); diff == "" {
			rw.mu.RUnlock()
			rw.t.Logf("✓ Resource already matched: %s %s/%s", kind, evt.Namespace, evt.Name)
			return nil
		}
	}
	rw.mu.RUnlock()

	// Step 2: Not found in existing events, subscribe to new events
	subCh := make(chan ResourceEvent, 100)

	// Register subscriber
	rw.mu.Lock()
	rw.subscribers = append(rw.subscribers, subCh)
	rw.mu.Unlock()

	// Cleanup subscriber on exit
	defer func() {
		rw.mu.Lock()
		for i, ch := range rw.subscribers {
			if ch == subCh {
				rw.subscribers = append(rw.subscribers[:i], rw.subscribers[i+1:]...)
				break
			}
		}
		rw.mu.Unlock()
		close(subCh)
	}()

	// Step 3: Wait for matching event or timeout
	deadline := time.Now().Add(timeout)
	var lastDiff string

	for {
		select {
		case evt, ok := <-subCh:
			if !ok {
				// Channel closed (context cancelled)
				return fmt.Errorf("watcher stopped")
			}

			// Only check events of the matching kind
			if evt.Kind != kind {
				continue
			}

			// Compare using go-cmp
			diff := cmp.Diff(expected, evt.Object, cmpOpts...)
			if diff == "" {
				// Match found!
				rw.t.Logf("✓ Resource matched: %s %s/%s", kind, evt.Namespace, evt.Name)
				return nil
			}

			// Store last diff for error reporting
			lastDiff = diff
			rw.t.Logf("Resource %s %s/%s not yet matching (waiting...)", kind, evt.Namespace, evt.Name)

		case <-time.After(time.Until(deadline)):
			if !time.Now().Before(deadline) {
				// Timeout
				if lastDiff != "" {
					return fmt.Errorf("timeout waiting for %s to match.\nLast diff (-want +got):\n%s", kind, lastDiff)
				}
				return fmt.Errorf("timeout waiting for %s (no events of this kind received)", kind)
			}
		}
	}
}

// WaitForKind waits for at least one event of the specified kind.
// Returns the first matching event, or error on timeout.
func (rw *ResourceWatcher) WaitForKind(kind string, timeout time.Duration) (*ResourceEvent, error) {
	rw.t.Helper()

	// Step 1: Check existing events first
	rw.mu.RLock()
	for _, evt := range rw.events {
		if evt.Kind == kind {
			result := evt
			rw.mu.RUnlock()
			rw.t.Logf("✓ Found existing %s: %s/%s", kind, evt.Namespace, evt.Name)
			return &result, nil
		}
	}
	rw.mu.RUnlock()

	// Step 2: Subscribe to new events
	subCh := make(chan ResourceEvent, 100)

	rw.mu.Lock()
	rw.subscribers = append(rw.subscribers, subCh)
	rw.mu.Unlock()

	defer func() {
		rw.mu.Lock()
		for i, ch := range rw.subscribers {
			if ch == subCh {
				rw.subscribers = append(rw.subscribers[:i], rw.subscribers[i+1:]...)
				break
			}
		}
		rw.mu.Unlock()
		close(subCh)
	}()

	// Step 3: Wait for matching event
	deadline := time.Now().Add(timeout)

	for {
		select {
		case evt, ok := <-subCh:
			if !ok {
				return nil, fmt.Errorf("watcher stopped")
			}

			if evt.Kind == kind {
				rw.t.Logf("✓ Found %s: %s/%s", kind, evt.Namespace, evt.Name)
				return &evt, nil
			}

		case <-time.After(time.Until(deadline)):
			if !time.Now().Before(deadline) {
				return nil, fmt.Errorf("timeout waiting for %s event", kind)
			}
		}
	}
}

// watchResource sets up an informer for a resource type (internal helper).
func (rw *ResourceWatcher) watchResource(ctx context.Context, mgr manager.Manager, obj client.Object) error {
	rw.t.Helper()

	informer, err := mgr.GetCache().GetInformer(ctx, obj)
	if err != nil {
		return fmt.Errorf("failed to get informer: %w", err)
	}

	kind := extractKind(obj)

	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cObj := obj.(client.Object)
			rw.sendEvent("ADDED", kind, cObj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			cObj := newObj.(client.Object)
			rw.sendEvent("UPDATED", kind, cObj)
		},
		DeleteFunc: func(obj interface{}) {
			cObj := obj.(client.Object)
			rw.sendEvent("DELETED", kind, cObj)
		},
	})

	return err
}

// sendEvent sends an event to the channel (internal helper).
func (rw *ResourceWatcher) sendEvent(eventType, kind string, obj client.Object) {
	rw.t.Helper()

	event := ResourceEvent{
		Type:      eventType,
		Kind:      kind,
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Object:    obj.DeepCopyObject().(client.Object),
		Time:      time.Now(),
	}

	select {
	case rw.eventCh <- event:
		rw.t.Logf("[%s] %s %s/%s", eventType, kind, obj.GetNamespace(), obj.GetName())
	default:
		rw.t.Logf("Warning: event channel full, dropping event")
	}
}

// extractKind extracts a clean kind name from a client.Object (internal helper).
func extractKind(obj client.Object) string {
	kind := fmt.Sprintf("%T", obj)
	// Remove pointer prefix
	if len(kind) > 0 && kind[0] == '*' {
		kind = kind[1:]
	}
	// Extract just the type name after the last dot
	for i := len(kind) - 1; i >= 0; i-- {
		if kind[i] == '.' {
			return kind[i+1:]
		}
	}
	return kind
}
