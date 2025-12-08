package testutil

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestFindLatestEventFor_EmptyNamespace tests findLatestEventFor with empty namespace.
func TestFindLatestEventFor_EmptyNamespace(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

// TestFindLatestEvent_NoMatch tests findLatestEvent when no events match.
func TestFindLatestEvent_NoMatch(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// TestFindLatestEventFor_EdgeCases tests all conditional branches in findLatestEventFor.
func TestFindLatestEventFor_EdgeCases(t *testing.T) {
	t.Parallel()

	watcher := &ResourceWatcher{
		t: t,
		events: []ResourceEvent{
			{Kind: "Service", Name: "svc-a", Namespace: "ns-a"},
			{Kind: "Service", Name: "svc-b", Namespace: "ns-b"},
			{Kind: "Pod", Name: "pod-a", Namespace: "ns-a"},
		},
	}

	tests := map[string]struct {
		setupObj  func() client.Object
		wantFound bool
	}{
		"kind mismatch": {
			setupObj: func() client.Object {
				o := &corev1.ConfigMap{}
				o.SetName("svc-a")
				o.SetNamespace("ns-a")
				return o
			},
			wantFound: false,
		},
		"name mismatch": {
			setupObj: func() client.Object {
				o := &corev1.Service{}
				o.SetName("svc-nonexist")
				o.SetNamespace("ns-a")
				return o
			},
			wantFound: false,
		},
		"namespace mismatch": {
			setupObj: func() client.Object {
				o := &corev1.Service{}
				o.SetName("svc-a")
				o.SetNamespace("ns-nonexist")
				return o
			},
			wantFound: false,
		},
		"match found": {
			setupObj: func() client.Object {
				o := &corev1.Service{}
				o.SetName("svc-b")
				o.SetNamespace("ns-b")
				return o
			},
			wantFound: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evt := watcher.findLatestEventFor(tc.setupObj())
			if (evt != nil) != tc.wantFound {
				t.Errorf("findLatestEventFor() found=%v, want=%v", evt != nil, tc.wantFound)
			}
		})
	}
}
