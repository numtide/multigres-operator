package testutil

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// Predefined error types for event waiting
var (
	// ErrKeepWaiting is a sentinel error that the predicate can return to
	// indicate it wants to continue waiting for more events.
	ErrKeepWaiting = errors.New("continue waiting for matching event")
)

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
	mu             sync.RWMutex
	events         []ResourceEvent
	eventCh        chan ResourceEvent
	subscribers    []chan ResourceEvent // Fan-out channels for WaitForMatch
	t              testing.TB
	extraResources []client.Object
	timeout        time.Duration          // Default timeout for WaitForMatch operations
	cmpOpts        []cmp.Option           // Default comparison options for WaitForMatch
	watchedKinds   map[string]interface{} // Tracks which resource kinds are being watched
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

// WithTimeout sets the default timeout for WaitForMatch operations.
// If not set, defaults to 5 seconds.
func WithTimeout(timeout time.Duration) Option {
	return func(rw *ResourceWatcher) error {
		rw.timeout = timeout
		return nil
	}
}

// WithCmpOpts sets the default comparison options for WaitForMatch operations.
// These options are passed to go-cmp's Diff function.
func WithCmpOpts(opts ...cmp.Option) Option {
	return func(rw *ResourceWatcher) error {
		rw.cmpOpts = opts
		return nil
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
		events:       []ResourceEvent{},
		eventCh:      make(chan ResourceEvent, 1000),
		t:            t,
		timeout:      5 * time.Second,              // Default timeout
		cmpOpts:      nil,                          // Default: no special comparison options
		watchedKinds: make(map[string]interface{}), // Initialize watched kinds tracker
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

// EventCh returns the channel for receiving events directly.
// Useful for custom event processing logic.
func (rw *ResourceWatcher) EventCh() <-chan ResourceEvent {
	rw.t.Helper()
	return rw.eventCh
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
	rw.timeout = 5 * time.Second
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

// waitForEvent is a helper that waits for an event matching the predicate function.
// It handles the common select/timeout logic and returns the matching event or an error.
//
// The predicate function should return an error indicating the action to take:
// - nil: match found, stop waiting and return the event successfully
// - ErrKeepWaiting: continue waiting for more events
// - any other error: stop waiting and return that error to the caller
//
// Returns:
// - (*ResourceEvent, nil): when predicate returns nil (match found)
// - (nil, context.Canceled): when the subscription channel is closed (watcher stopped)
// - (nil, context.DeadlineExceeded): when the deadline is reached
// - (nil, error): when predicate returns an error other than ErrKeepWaiting
func waitForEvent(
	t testing.TB,
	subCh chan ResourceEvent,
	deadline time.Time,
	predicate func(ResourceEvent) error,
) (*ResourceEvent, error) {
	t.Helper()

	for {
		select {
		case evt, ok := <-subCh:
			if !ok {
				// Channel closed (context cancelled)
				return nil, context.Canceled
			}

			err := predicate(evt)
			if err == nil {
				// Match found
				return &evt, nil
			}
			if errors.Is(err, ErrKeepWaiting) {
				// Continue waiting for more events
				continue
			}
			// Any other error, stop and return it
			return nil, err

		case <-time.After(time.Until(deadline)):
			if !time.Now().Before(deadline) {
				// Timeout reached
				return nil, context.DeadlineExceeded
			}
		}
	}
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

// WaitForMatch waits for one or more resources to match the expected objects
// using go-cmp comparison. Returns nil when all matched, error on timeout.
//
// Uses the watcher's configured timeout and comparison options (set via SetTimeout/SetCmpOpts
// or during initialization with WithTimeout/WithCmpOpts).
//
// The timeout applies to the entire operation, not per resource. All resources share
// the same deadline.
//
// First checks existing events for early return, then subscribes to new events.
//
// When multiple objects are provided, waits for all of them to match.
//
// Example:
//
//	watcher.SetCmpOpts(testutil.CompareSpecOnly()...)
//	expectedSts := &appsv1.StatefulSet{
//	    Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3))},
//	}
//	expectedSvc := &corev1.Service{...}
//	err := watcher.WaitForMatch(expectedSts, expectedSvc)
//	if err != nil {
//	    t.Errorf("Resources never reached expected state: %v", err)
//	}
func (rw *ResourceWatcher) WaitForMatch(expected ...client.Object) error {
	rw.t.Helper()

	if len(expected) == 0 {
		return nil
	}

	// Validate all kinds are being watched before waiting
	var unwatchedKinds []string
	for _, obj := range expected {
		kind := extractKind(obj)
		if _, watched := rw.watchedKinds[kind]; !watched {
			unwatchedKinds = append(unwatchedKinds, kind)
		}
	}
	if len(unwatchedKinds) > 0 {
		return &ErrUnwatchedKinds{Kinds: unwatchedKinds}
	}

	// Calculate deadline once for all objects
	deadline := time.Now().Add(rw.timeout)
	cmpOpts := rw.cmpOpts

	// Wait for each object to match using shared deadline
	for _, obj := range expected {
		if err := rw.waitForSingleMatch(obj, deadline, cmpOpts); err != nil {
			return err
		}
	}

	return nil
}

// waitForSingleMatch waits for a single resource to match the expected object.
func (rw *ResourceWatcher) waitForSingleMatch(
	expected client.Object,
	deadline time.Time,
	cmpOpts []cmp.Option,
) error {
	rw.t.Helper()

	kind := extractKind(expected)

	// Step 1: Check latest state of resource in existing events
	matched, diff := rw.checkLatestEventMatches(expected, cmpOpts)
	if matched {
		return nil
	}
	if diff != "" {
		if showDiffs {
			rw.t.Logf("Exists but not matching \"%s\", subscribing for updates...\n%v", kind, diff)
		} else {
			rw.t.Logf("Exists but not matching \"%s\", subscribing for updates...", kind)
		}
	}

	// Step 2: Subscribe to new events
	subCh := rw.subscribe()
	defer rw.unsubscribe(subCh)

	// Step 3: Wait for matching event or timeout (using shared deadline)
	// Initialize lastDiff with the diff from initial check (if any)
	lastDiff := diff

	predicate := func(evt ResourceEvent) error {
		// Only check events of the matching kind
		if evt.Kind != kind {
			return ErrKeepWaiting
		}

		// Compare using go-cmp
		diff := cmp.Diff(expected, evt.Object, cmpOpts...)
		if diff == "" {
			// Match found!
			rw.t.Logf("Matched \"%s\" %s/%s", kind, evt.Namespace, evt.Name)
			return nil
		}

		// Store last diff for error reporting
		lastDiff = diff
		if showDiffs {
			rw.t.Logf("Waiting for \"%s\" %s/%s:\n%v", kind, evt.Namespace, evt.Name, diff)
		} else {
			rw.t.Logf("Waiting for \"%s\" %s/%s", kind, evt.Namespace, evt.Name)
		}
		return ErrKeepWaiting
	}

	_, err := waitForEvent(rw.t, subCh, deadline, predicate)
	if err != nil {
		// Customize error message for timeout with diff details
		if errors.Is(err, context.DeadlineExceeded) {
			if lastDiff != "" {
				return fmt.Errorf(
					"timeout waiting for %s to match.\nLast diff (-want +got):\n%s",
					kind,
					lastDiff,
				)
			}
			return fmt.Errorf("timeout waiting for %s (no events of this kind received)", kind)
		}
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("watcher stopped")
		}
		return err
	}

	return nil
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
			rw.t.Logf("Found \"%s\" %s/%s", kind, evt.Namespace, evt.Name)
			return &result, nil
		}
	}
	rw.mu.RUnlock()

	// Step 2: Subscribe to new events
	subCh := rw.subscribe()
	defer rw.unsubscribe(subCh)

	// Step 3: Wait for matching event
	deadline := time.Now().Add(timeout)

	for {
		select {
		case evt, ok := <-subCh:
			if !ok {
				return nil, fmt.Errorf("watcher stopped")
			}

			if evt.Kind == kind {
				rw.t.Logf("Found \"%s\" %s/%s", kind, evt.Namespace, evt.Name)
				return &evt, nil
			}

		case <-time.After(time.Until(deadline)):
			if !time.Now().Before(deadline) {
				return nil, fmt.Errorf("timeout waiting for \"%s\" event", kind)
			}
		}
	}
}

// WaitForEventType waits for an event with specific kind and type (ADDED, UPDATED, DELETED).
// Returns the first matching event, or error on timeout.
func (rw *ResourceWatcher) WaitForEventType(
	kind, eventType string,
	timeout time.Duration,
) (*ResourceEvent, error) {
	rw.t.Helper()

	// Step 1: Check existing events first
	rw.mu.RLock()
	for _, evt := range rw.events {
		if evt.Kind == kind && evt.Type == eventType {
			result := evt
			rw.mu.RUnlock()
			rw.t.Logf("Found %s \"%s\" %s/%s", eventType, kind, evt.Namespace, evt.Name)
			return &result, nil
		}
	}
	rw.mu.RUnlock()

	// Step 2: Subscribe to new events
	subCh := rw.subscribe()
	defer rw.unsubscribe(subCh)

	// Step 3: Wait for matching event
	deadline := time.Now().Add(timeout)

	for {
		select {
		case evt, ok := <-subCh:
			if !ok {
				return nil, fmt.Errorf("watcher stopped")
			}

			if evt.Kind == kind && evt.Type == eventType {
				rw.t.Logf("Found %s \"%s\" %s/%s", eventType, kind, evt.Namespace, evt.Name)
				return &evt, nil
			}

		case <-time.After(time.Until(deadline)):
			if !time.Now().Before(deadline) {
				return nil, fmt.Errorf("timeout waiting for %s \"%s\" event", eventType, kind)
			}
		}
	}
}

// watchResource sets up an informer for a resource type (internal helper).
func (rw *ResourceWatcher) watchResource(
	ctx context.Context,
	mgr manager.Manager,
	obj client.Object,
) error {
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
	if err != nil {
		return err
	}

	// Track this kind as watched
	rw.watchedKinds[kind] = nil

	return nil
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
		rw.t.Logf(
			"(%s) \"%s\" %s/%s",
			strings.ToLower(eventType),
			kind,
			obj.GetNamespace(),
			obj.GetName(),
		)
	default:
		rw.t.Logf("Warning: event channel full, dropping event")
	}
}

// findLatestEventFor finds the most recent event matching the given object.
// If the object has empty name and namespace, it matches by kind only.
// Returns nil if no matching event found.
func (rw *ResourceWatcher) findLatestEventFor(obj client.Object) *ResourceEvent {
	rw.t.Helper()

	kind := extractKind(obj)
	name := obj.GetName()
	namespace := obj.GetNamespace()

	rw.mu.RLock()
	defer rw.mu.RUnlock()

	// Iterate backwards to find latest event first
	for i := len(rw.events) - 1; i >= 0; i-- {
		evt := rw.events[i]

		// Must match kind
		if evt.Kind != kind {
			continue
		}

		// If name is specified, must match name
		if name != "" && evt.Name != name {
			continue
		}

		// If namespace is specified, must match namespace
		if namespace != "" && evt.Namespace != namespace {
			continue
		}

		// Found a match
		return &evt
	}

	return nil
}

// checkLatestEventMatches finds the latest event for the expected object and compares it.
// Returns (matched, diff). If no event found, returns (false, "").
func (rw *ResourceWatcher) checkLatestEventMatches(
	expected client.Object,
	cmpOpts []cmp.Option,
) (bool, string) {
	rw.t.Helper()

	latestEvt := rw.findLatestEventFor(expected)
	if latestEvt == nil {
		return false, ""
	}

	diff := cmp.Diff(expected, latestEvt.Object, cmpOpts...)
	if diff == "" {
		rw.t.Logf(
			"Matched \"%s\" %s/%s (from existing events)",
			latestEvt.Kind,
			latestEvt.Namespace,
			latestEvt.Name,
		)
		return true, ""
	}

	return false, diff
}

// subscribe creates and registers a new subscriber channel for fan-out.
func (rw *ResourceWatcher) subscribe() chan ResourceEvent {
	subCh := make(chan ResourceEvent, 100)

	rw.mu.Lock()
	rw.subscribers = append(rw.subscribers, subCh)
	rw.mu.Unlock()

	return subCh
}

// unsubscribe removes and closes a subscriber channel.
func (rw *ResourceWatcher) unsubscribe(subCh chan ResourceEvent) {
	rw.mu.Lock()
	for i, ch := range rw.subscribers {
		if ch == subCh {
			rw.subscribers = append(rw.subscribers[:i], rw.subscribers[i+1:]...)
			break
		}
	}
	rw.mu.Unlock()

	close(subCh)
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
