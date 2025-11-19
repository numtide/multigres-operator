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
		t:            t,
		timeout:      5 * time.Second,      // Default timeout
		cmpOpts:      nil,                  // Default: no special comparison options
		watchedKinds: make(map[string]any), // Initialize watched kinds tracker
		events:       []ResourceEvent{},
		eventCh:      make(chan ResourceEvent, 1000),
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

// Events returns a snapshot of all collected events at the current time.
func (rw *ResourceWatcher) Events() []ResourceEvent {
	rw.t.Helper()

	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return append([]ResourceEvent{}, rw.events...)
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

// waitForEvent is a helper that waits for an event matching the predicate
// function. It handles the common select/timeout logic and returns the matching
// event or an error.
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
//
// TODO: Currently there is no use of the matched event, maybe it's someting we
// can drop.
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

// collectEvents collects events and fanning out to subscribers.
//
// This is meant to be run in the background using goroutines.
func (rw *ResourceWatcher) collectEvents(ctx context.Context) {
	rw.t.Helper()

	for {
		select {
		case evt := <-rw.eventCh:
			rw.mu.Lock()
			// Store in the events slice for cache.
			rw.events = append(rw.events, evt)

			// Fan out to all subscribers.
			for _, subCh := range rw.subscribers {
				select {
				case subCh <- evt:
					// Event sent to subscriber.
				default:
					// When subscriber channel is full, skip.
					rw.t.Logf("Warning: subscriber channel full, dropping event")
				}
			}
			rw.mu.Unlock()
		case <-ctx.Done():
			// Close all subscriber channels.
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
// Uses the watcher's configured timeout and comparison options (set via
// SetTimeout/SetCmpOpts or during initialization with WithTimeout/WithCmpOpts).
//
// The timeout applies to the entire operation, not per resource. All resources
// share the same deadline.
//
// First checks existing events for early return, then subscribes to new events.
// Note that, when the desired state is found, this terminates prematurely
// regardless of how the future events change the actual state of the object.
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

	// Validate all provided kinds are being watched before waiting, and if any
	// is missing, return early with an error.
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

	// Calculate deadline once for all objects.
	deadline := time.Now().Add(rw.timeout)
	// Note that this is also copied so that long running subscription won't
	// refer to other cmpOpts which can be updated while running.
	cmpOpts := rw.cmpOpts

	// Wait for each object to match using shared deadline.
	// Note how this does not run checks concurrently for simplicity. All the
	// events are stored in the events cache slice, and thus starting
	// sequentially would still result in the match against the latest event.
	// There could be some potential nuance where object getting updated after
	// the match is found, and this does not cover such use cases.
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

	// Step 1: Check latest state of resource in existing events.
	matched, diff := rw.checkLatestEventMatches(expected, cmpOpts)
	if matched {
		return nil
	}
	if diff != "" {
		suffix := ""
		// When the build flag is specified, include the diff as suffix.
		if showDiffs {
			suffix = "\n" + diff
		}
		rw.t.Logf("Exists but not matching \"%s\", subscribing for updates...%s", kind, suffix)
	}

	// Step 2: Subscribe to new events.
	subCh := rw.subscribe()
	defer rw.unsubscribe(subCh)

	// Step 3: Wait for matching event or timeout (using shared deadline).
	// Initialize lastDiff with the diff from initial check (if any).
	lastDiff := diff

	predicate := func(evt ResourceEvent) error {
		// Only check events of the matching kind.
		if evt.Kind != kind {
			return ErrKeepWaiting
		}

		// Compare using go-cmp.
		diff := cmp.Diff(expected, evt.Object, cmpOpts...)
		if diff == "" {
			rw.t.Logf("Matched \"%s\" %s/%s", kind, evt.Namespace, evt.Name)
			return nil
		}

		// Store last diff for error reporting.
		lastDiff = diff
		// Log verbosity is handled by build flag of "verbose".

		suffix := ""
		// When the build flag is specified, include the diff as suffix.
		if showDiffs {
			suffix = "\n" + diff
		}
		rw.t.Logf("Waiting for \"%s\" %s/%s%s", kind, evt.Namespace, evt.Name, suffix)

		return ErrKeepWaiting
	}

	_, err := waitForEvent(rw.t, subCh, deadline, predicate)
	if err != nil {
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

	// Match found, return nil.
	// TODO: We could return the matched object, but not necessary with the
	// current logic.
	return nil
}

// WaitForEventType waits for an event with specific kind and type
// (ADDED, UPDATED, DELETED). Returns the first matching event, or error on
// timeout.
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

// watchResource sets up an informer for a resource type.
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
		AddFunc: func(obj any) {
			cObj := obj.(client.Object)
			rw.sendEvent("ADDED", kind, cObj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			cObj := newObj.(client.Object)
			rw.sendEvent("UPDATED", kind, cObj)
		},
		DeleteFunc: func(obj any) {
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

// sendEvent sends an event to the channel, fallback to skip when the channel
// cannot receive.
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

// findLatestEvent searches for an event matching the predicate function (newest first).
// The predicate is called with each event (newest to oldest) while holding the read lock.
// Returns the first matching event, or nil if none found.
func (rw *ResourceWatcher) findLatestEvent(predicate func(ResourceEvent) bool) *ResourceEvent {
	rw.t.Helper()

	rw.mu.RLock()
	defer rw.mu.RUnlock()

	for i := len(rw.events) - 1; i >= 0; i-- {
		if predicate(rw.events[i]) {
			evt := rw.events[i]
			return &evt
		}
	}
	return nil
}

// findLatestEventFor finds the most recent event matching the given object.
// If the object has empty name and namespace, it matches by kind only.
// Returns nil if no matching event found.
func (rw *ResourceWatcher) findLatestEventFor(obj client.Object) *ResourceEvent {
	rw.t.Helper()

	kind := extractKind(obj)
	name := obj.GetName()
	namespace := obj.GetNamespace()

	return rw.findLatestEvent(func(evt ResourceEvent) bool {
		if evt.Kind != kind {
			return false
		}
		if name != "" && evt.Name != name {
			return false
		}
		if namespace != "" && evt.Namespace != namespace {
			return false
		}
		return true
	})
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
