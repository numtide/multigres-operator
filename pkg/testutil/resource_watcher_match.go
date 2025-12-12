package testutil

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

////----------------------------------------
///  Match watch
//------------------------------------------
// This file contains match watch handling.

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

	predicate := func(evt ResourceEvent) bool {
		// Only check events of the matching kind.
		if evt.Kind != kind {
			return false
		}

		// Compare using go-cmp.
		diff := cmp.Diff(expected, evt.Object, cmpOpts...)
		if diff == "" {
			rw.t.Logf("Matched \"%s\" %s/%s", kind, evt.Namespace, evt.Name)
			return true
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

		return false
	}

	_, err := rw.waitForEvent(subCh, deadline, predicate)
	if err != nil {
		// Enhance timeout errors with diff information if available
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
		// For other errors (like context.Canceled), wrap and return
		return fmt.Errorf("waiting for %s to match: %w", kind, err)
	}

	// Match found, return nil.
	// TODO: We could return the matched object, but not necessary with the
	// current logic.
	return nil
}

// waitForEvent is a helper that waits for an event matching the predicate
// function. It handles the common select/timeout logic and returns the matching
// event or an error.
//
// The predicate function returns true when a match is found, false to keep waiting.
//
// Returns:
// - (*ResourceEvent, nil): when predicate returns true (match found)
// - (nil, context.Canceled): when the subscription channel is closed (watcher stopped)
// - (nil, context.DeadlineExceeded): when the deadline is reached
//
// TODO: Currently there is no use of the matched event, maybe it's someting we
// can drop.
func (rw *ResourceWatcher) waitForEvent(
	subCh chan ResourceEvent,
	deadline time.Time,
	predicate func(ResourceEvent) bool,
) (*ResourceEvent, error) {
	rw.t.Helper()

	for {
		select {
		case evt, ok := <-subCh:
			if !ok {
				// Channel closed (context cancelled)
				return nil, context.Canceled
			}

			if predicate(evt) {
				// Match found
				return &evt, nil
			}
			// No match, continue waiting

		case <-time.After(time.Until(deadline)):
			if !time.Now().Before(deadline) {
				// Timeout reached
				return nil, context.DeadlineExceeded
			}
		}
	}
}

// WaitForEventType waits for an event with specific kind and type
// (ADDED, UPDATED, DELETED). Returns the first matching event, or error on
// timeout.
//
// Uses the watcher's configured timeout (set via SetTimeout or WithTimeout).
func (rw *ResourceWatcher) WaitForEventType(
	kind, eventType string,
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
	deadline := time.Now().Add(rw.timeout)

	predicate := func(evt ResourceEvent) bool {
		if evt.Kind == kind && evt.Type == eventType {
			rw.t.Logf("Found %s \"%s\" %s/%s", eventType, kind, evt.Namespace, evt.Name)
			return true
		}
		return false
	}

	evt, err := rw.waitForEvent(subCh, deadline, predicate)
	if err != nil {
		return nil, fmt.Errorf("waiting for %s \"%s\" event: %w", eventType, kind, err)
	}
	return evt, nil
}

// eventHandlerRegistrar abstracts the AddEventHandler method for testing.
type eventHandlerRegistrar interface {
	AddEventHandler(
		handler cache.ResourceEventHandler,
	) (cache.ResourceEventHandlerRegistration, error)
}

// addEventHandlerToInformer registers the event handlers to the informer.
// Extracted for testing purposes.
func (rw *ResourceWatcher) addEventHandlerToInformer(
	informer eventHandlerRegistrar,
	kind string,
) {
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
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
		rw.t.Fatalf("Failed to add event handler to informer: %v", err)
	}
}

// watchResource sets up an informer for a resource type.
func (rw *ResourceWatcher) watchResource(
	ctx context.Context,
	mgr manager.Manager,
	obj client.Object,
) {
	rw.t.Helper()

	kind := extractKind(obj)

	// Check if already watching this kind
	if _, watched := rw.watchedKinds[kind]; watched {
		return // Already watching, skip
	}

	informer, err := mgr.GetCache().GetInformer(ctx, obj)
	if err != nil {
		rw.t.Fatalf("Failed to get informer: %v", err)
		return
	}

	rw.addEventHandlerToInformer(informer, kind)

	// Track this kind as watched
	rw.watchedKinds[kind] = nil
}
