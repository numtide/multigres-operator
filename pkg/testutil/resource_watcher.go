package testutil

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

////========================================
///  Resource Watcher
//==========================================
// The struct ResourceWatcher and its method provides a simple and flexible
// interface for checking the resource status in envtest Kubernetes cluster.
//
// By using methods such as WaitForMatch, or WaitForDeletion, you can test the
// controller logic without adding any arbitrary wait / poll logic, and check
// for the exact object match based on go-cmp's diffing.
//
// Because there are various scenarios for watching resources, and because it
// involves both caching logic as well as realtime event handling, the code is
// broken up into multiple resource_watcher_.*.go.

// ErrUnwatchedKinds is returned when trying to wait for resource kinds
// that aren't being watched by the ResourceWatcher.
type ErrUnwatchedKinds struct {
	Kinds []string
}

func (e *ErrUnwatchedKinds) Error() string {
	return fmt.Sprintf(
		"the following kinds are not being watched by this ResourceWatcher: %v",
		e.Kinds,
	)
}

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
	t testing.TB

	// Mutex is used for subscription mechanism, as well as "events" slice for
	// historical events.
	mu sync.RWMutex

	timeout time.Duration // Default timeout for WaitForMatch operations
	cmpOpts []cmp.Option  // Default comparison options for WaitForMatch

	extraResources []client.Object
	watchedKinds   map[string]any // Tracks which resource kinds are being watched
	events         []ResourceEvent
	eventCh        chan ResourceEvent

	// subscribers set up with a simple slice and loop over the channels. This
	// may not be the most performant for lookup, but given that there shouldn't
	// be too many subscribers in action, sticking with this approach.
	subscribers []chan ResourceEvent // Fan-out channels for WaitForMatch
}

type Option func(rw *ResourceWatcher)

const (
	defautTimeout = 5 * time.Second
)

// WithExtraResource adds a watch for an additional resource type. The object
// should be a pointer reference to the struct such as a custom resource.
//
// If you need to watch multiple resources, you can provide the list of
// resources.
func WithExtraResource(objs ...client.Object) Option {
	return func(rw *ResourceWatcher) {
		rw.extraResources = append(rw.extraResources, objs...)
	}
}

// WithTimeout sets the default timeout for WaitForMatch operations.
// If not set, defaults to 5 seconds.
func WithTimeout(timeout time.Duration) Option {
	return func(rw *ResourceWatcher) {
		rw.timeout = timeout
	}
}

// WithCmpOpts sets the default comparison options for WaitForMatch operations.
// These options are passed to go-cmp's Diff function.
func WithCmpOpts(opts ...cmp.Option) Option {
	return func(rw *ResourceWatcher) {
		rw.cmpOpts = opts
	}
}

// NewResourceWatcher creates a new ResourceWatcher and automatically watches
// Service, StatefulSet, and Deployment resources.
func NewResourceWatcher(
	t testing.TB,
	ctx context.Context,
	mgr manager.Manager,
	opts ...Option,
) *ResourceWatcher {
	t.Helper()

	watcher := &ResourceWatcher{
		t:            t,
		timeout:      defautTimeout,
		cmpOpts:      nil, // Default: no special comparison options
		watchedKinds: make(map[string]any),
		events:       []ResourceEvent{},
		eventCh:      make(chan ResourceEvent, 1000),
	}
	for _, o := range opts {
		o(watcher)
	}

	// Start background collector.
	go watcher.collectEvents(ctx)

	// Automatically watch standard resources.
	watcher.watchResource(ctx, mgr, &corev1.Service{})
	watcher.watchResource(ctx, mgr, &appsv1.StatefulSet{})
	watcher.watchResource(ctx, mgr, &appsv1.Deployment{})

	// Watch extra resources provided.
	for _, res := range watcher.extraResources {
		watcher.watchResource(ctx, mgr, res)
	}

	return watcher
}

// SetTimeout updates the default timeout for WaitForMatch operations.
// This can be called at any time to change the timeout for subsequent calls.
func (rw *ResourceWatcher) SetTimeout(timeout time.Duration) {
	rw.t.Helper()
	rw.timeout = timeout
}

// ResetTimeout resets the timeout to the default value (5 seconds).
func (rw *ResourceWatcher) ResetTimeout() {
	rw.t.Helper()
	rw.timeout = defautTimeout
}

// SetCmpOpts updates the default comparison options for WaitForMatch operations.
// This can be called at any time to change the options for subsequent calls.
func (rw *ResourceWatcher) SetCmpOpts(opts ...cmp.Option) {
	rw.t.Helper()
	rw.cmpOpts = opts
}

// ResetCmpOpts resets the comparison options to nil (no special options).
func (rw *ResourceWatcher) ResetCmpOpts() {
	rw.t.Helper()
	rw.cmpOpts = nil
}

// Events returns a snapshot of all collected events at the current time.
//
// This provides a low level control to check the events directly, but in most
// cases, there are other functions that can handle more robustly, such as
// WaitForMatch.
func (rw *ResourceWatcher) Events() []ResourceEvent {
	rw.t.Helper()

	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return append([]ResourceEvent{}, rw.events...)
}

// EventCh returns the channel for receiving events directly.
//
// This provides a low level control to check the events directly, but in most
// cases, there are other functions that can handle more robustly, such as
// WaitForMatch.
func (rw *ResourceWatcher) EventCh() <-chan ResourceEvent {
	rw.t.Helper()
	return rw.eventCh
}

// ForKind returns events for a specific resource kind.
//
// This provides a low level control to check the events directly, but in most
// cases, there are other functions that can handle more robustly, such as
// WaitForMatch.
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
//
// This provides a low level control to check the events directly, but in most
// cases, there are other functions that can handle more robustly, such as
// WaitForMatch.
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

// subscribe creates and registers a new subscriber channel for fan-out.
func (rw *ResourceWatcher) subscribe() chan ResourceEvent {
	rw.t.Helper()

	// NOTE: Arbitrary buffer set
	subCh := make(chan ResourceEvent, 100)

	rw.mu.Lock()
	rw.subscribers = append(rw.subscribers, subCh)
	rw.mu.Unlock()

	return subCh
}

// unsubscribe removes and closes a subscriber channel.
func (rw *ResourceWatcher) unsubscribe(subCh chan ResourceEvent) {
	rw.t.Helper()

	rw.mu.Lock()
	defer rw.mu.Unlock()

	// If subscribers is nil, collectEvents already closed all channels
	if rw.subscribers == nil {
		return
	}

	for i, ch := range rw.subscribers {
		if ch == subCh {
			rw.subscribers = append(rw.subscribers[:i], rw.subscribers[i+1:]...)
			close(subCh)
			return
		}
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
	parts := strings.Split(kind, ".")
	return parts[len(parts)-1]
}
