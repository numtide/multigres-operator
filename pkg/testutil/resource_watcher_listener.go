package testutil

import (
	"context"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

////----------------------------------------
///  Event listener
//------------------------------------------
// This file contains event listening logic.

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
			// Close all subscriber channels to signal waiting functions to stop
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
