package testutil

import (
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

////----------------------------------------
///  Deletion watch
//------------------------------------------
// This file contains deletion watch handling.

// Obj creates a client.Object with the given name and namespace.
// This is a convenience helper for deletion testing and other scenarios
// where you need to reference an object by name/namespace only.
//
// Example:
//
//	watcher.WaitForDeletion(testutil.Obj[appsv1.StatefulSet]("etcd", "default"))
func Obj[T any, PT interface {
	*T
	client.Object
}](name, namespace string) PT {
	obj := new(T)
	ptr := PT(obj)
	ptr.SetName(name)
	ptr.SetNamespace(namespace)
	return ptr
}

// WaitForDeletion waits for one or more resources to be deleted (receive DELETED events).
// This checks that resources were fully removed from the cluster, not just
// marked for deletion with DeletionTimestamp.
//
// Uses the watcher's configured timeout. The timeout applies to the entire operation,
// not per resource. All resources share the same deadline.
//
// Example:
//
//	// Delete parent resource
//	client.Delete(ctx, etcd)
//
//	// Wait for owned resources to be cascade deleted
//	err := watcher.WaitForDeletion(
//	    testutil.Obj[appsv1.StatefulSet]("etcd", "default"),
//	    testutil.Obj[corev1.Service]("etcd", "default"),
//	    testutil.Obj[corev1.Service]("etcd-headless", "default"),
//	)
func (rw *ResourceWatcher) WaitForDeletion(objs ...client.Object) error {
	rw.t.Helper()

	if len(objs) == 0 {
		return nil
	}

	deadline := time.Now().Add(rw.timeout)

	for _, obj := range objs {
		if err := rw.waitForSingleDeletion(obj, deadline); err != nil {
			return err
		}
	}

	return nil
}

// waitForSingleDeletion waits for a single resource to be deleted.
func (rw *ResourceWatcher) waitForSingleDeletion(obj client.Object, deadline time.Time) error {
	rw.t.Helper()

	kind := extractKind(obj)
	name := obj.GetName()
	namespace := obj.GetNamespace()

	predicate := func(evt ResourceEvent) bool {
		return evt.Kind == kind && evt.Name == name && evt.Namespace == namespace && evt.Type == "DELETED"
	}

	// Subscribe BEFORE checking existing events to avoid TOCTOU race:
	// without this, an event could arrive between findLatestEvent and
	// subscribe, and be missed by both.
	subCh := rw.subscribe()
	defer rw.unsubscribe(subCh)

	// Check existing events (already in cache).
	if evt := rw.findLatestEvent(predicate); evt != nil {
		rw.t.Logf("Matched DELETED \"%s\" %s/%s (from existing events)", kind, namespace, name)
		return nil
	}

	// Wait for new DELETED event via subscription.
	matchPredicate := func(evt ResourceEvent) bool {
		if predicate(evt) {
			rw.t.Logf("Matched DELETED \"%s\" %s/%s", kind, namespace, name)
			return true
		}
		return false
	}

	_, err := rw.waitForEvent(subCh, deadline, matchPredicate)
	if err != nil {
		return fmt.Errorf("waiting for %s %s/%s to be deleted: %w", kind, namespace, name, err)
	}
	return nil
}
