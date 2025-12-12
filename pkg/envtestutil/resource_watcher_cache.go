package envtestutil

import (
	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

////----------------------------------------
///  Cache based check
//------------------------------------------
// This file contains the checks done based on the cached events.

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
