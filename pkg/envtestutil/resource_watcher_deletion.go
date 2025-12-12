package envtestutil

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

	// Check existing events first
	if evt := rw.findLatestEvent(func(e ResourceEvent) bool {
		return e.Kind == kind && e.Name == name && e.Namespace == namespace && e.Type == "DELETED"
	}); evt != nil {
		rw.t.Logf("Matched DELETED \"%s\" %s/%s (from existing events)", kind, namespace, name)
		return nil
	}

	// Subscribe to new events
	subCh := rw.subscribe()
	defer rw.unsubscribe(subCh)

	// Wait for DELETED event
	predicate := func(evt ResourceEvent) bool {
		if evt.Kind != kind || evt.Name != name || evt.Namespace != namespace {
			return false
		}
		if evt.Type == "DELETED" {
			rw.t.Logf("Matched DELETED \"%s\" %s/%s", kind, namespace, name)
			return true
		}
		return false
	}

	_, err := rw.waitForEvent(subCh, deadline, predicate)
	if err != nil {
		return fmt.Errorf("waiting for %s %s/%s to be deleted: %w", kind, namespace, name, err)
	}
	return nil
}
