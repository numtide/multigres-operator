//go:build integration && verbose
// +build integration,verbose

// Edge case tests that require the verbose build tag to achieve 100% coverage.
// These tests cover defensive error paths and rare scenarios that are difficult to
// trigger in normal usage but are important for complete test coverage.

package testutil

import (
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// testError is used for error injection testing.
var testError = errors.New("test error for coverage")

// TestVerboseDiffs_ExistingNonMatch tests verbose diff logging when existing event doesn't match.
func TestVerboseDiffs_ExistingNonMatch(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()

	// Create Service before watcher starts
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-svc", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
	if err := c.Create(ctx, svc); err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	watcher := NewResourceWatcher(t, ctx, mgr,
		WithTimeout(100*time.Millisecond),
	)

	time.Sleep(100 * time.Millisecond)

	// Try to match different spec - logs verbose diffs for existing event
	expected := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-svc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Protocol: corev1.ProtocolTCP, Port: 9999, TargetPort: intstr.FromInt(9999)},
			},
			Type:            corev1.ServiceTypeClusterIP,
			SessionAffinity: corev1.ServiceAffinityNone,
		},
	}

	watcher.SetCmpOpts(IgnoreMetaRuntimeFields(), IgnoreServiceRuntimeFields())

	err := watcher.WaitForMatch(expected)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

// TestVerboseDiffs_IncomingEvents tests verbose diff logging for incoming events.
func TestVerboseDiffs_IncomingEvents(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()

	watcher := NewResourceWatcher(t, ctx, mgr)
	watcher.SetCmpOpts(IgnoreMetaRuntimeFields(), IgnoreServiceRuntimeFields(),
		IgnoreStatefulSetRuntimeFields(), IgnoreDeploymentRuntimeFields())
	watcher.SetTimeout(600 * time.Millisecond)

	// Create resources while WaitForMatch is running
	go func() {
		time.Sleep(100 * time.Millisecond)

		// Create StatefulSet (different kind - will be skipped)
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-sts", Namespace: "default"},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "nginx", Image: "nginx"}}},
				},
			},
		}
		c.Create(ctx, sts)

		time.Sleep(80 * time.Millisecond)

		// Create Service with wrong spec
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-svc-v", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}
		c.Create(ctx, svc)

		// Update it multiple times (logs verbose diffs each time)
		for i := 0; i < 3; i++ {
			time.Sleep(80 * time.Millisecond)
			key := client.ObjectKey{Name: "test-svc-v", Namespace: "default"}
			if err := c.Get(ctx, key, svc); err == nil {
				svc.Spec.Ports[0].Port = int32(81 + i)
				c.Update(ctx, svc)
			}
		}
	}()

	// Wait for Service with spec that never matches
	expected := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-svc-v", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Protocol: corev1.ProtocolTCP, Port: 9999, TargetPort: intstr.FromInt(9999)},
			},
			Type:            corev1.ServiceTypeClusterIP,
			SessionAffinity: corev1.ServiceAffinityNone,
		},
	}

	err := watcher.WaitForMatch(expected)
	if err == nil {
		t.Error("Expected timeout")
	}
}

// TestDeletionPredicate_NonMatchingEvents tests deletion predicate with various event types.
func TestDeletionPredicate_NonMatchingEvents(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()

	watcher := NewResourceWatcher(t, ctx, mgr)
	watcher.SetCmpOpts(IgnoreMetaRuntimeFields(), IgnoreServiceRuntimeFields())
	watcher.SetTimeout(500 * time.Millisecond)

	go func() {
		time.Sleep(100 * time.Millisecond)

		// Create and update svc1 (UPDATED events won't match deletion predicate)
		svc1 := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-update", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}
		c.Create(ctx, svc1)

		time.Sleep(80 * time.Millisecond)

		key := client.ObjectKey{Name: "svc-update", Namespace: "default"}
		if err := c.Get(ctx, key, svc1); err == nil {
			svc1.Spec.Ports[0].Port = 81
			c.Update(ctx, svc1)
		}

		time.Sleep(80 * time.Millisecond)

		// Create and delete svc2 (DELETED event with wrong name)
		svc2 := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-other", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 82}}},
		}
		c.Create(ctx, svc2)
		time.Sleep(80 * time.Millisecond)
		c.Delete(ctx, svc2)
	}()

	// Wait for deletion of non-existent service (times out)
	err := watcher.WaitForDeletion(Obj[corev1.Service]("svc-target", "default"))
	if err == nil {
		t.Error("Expected timeout")
	}
}

// TestErrorInjection_MatchPredicate tests injected error in WaitForMatch.
func TestErrorInjection_MatchPredicate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()

	watcher := NewResourceWatcher(t, ctx, mgr)

	// Inject custom error
	watcher.testInjectMatchPredicateError = func() error {
		return testError
	}

	// Create Service in background
	go func() {
		time.Sleep(150 * time.Millisecond)
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-inject", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}
		c.Create(ctx, svc)
	}()

	expected := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-inject", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}

	watcher.SetCmpOpts(IgnoreMetaRuntimeFields(), IgnoreServiceRuntimeFields())

	err := watcher.WaitForMatch(expected)
	if !errors.Is(err, testError) {
		t.Errorf("Expected testError, got: %v", err)
	}
}

// TestErrorInjection_DeletePredicate tests injected error in WaitForDeletion.
func TestErrorInjection_DeletePredicate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()

	watcher := NewResourceWatcher(t, ctx, mgr)

	// Inject custom error
	watcher.testInjectDeletePredicateError = func() error {
		return testError
	}

	// Create Service in background
	go func() {
		time.Sleep(150 * time.Millisecond)
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-del-inject", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}
		c.Create(ctx, svc)

		time.Sleep(100 * time.Millisecond)

		// Update triggers predicate with matching kind/name/namespace
		key := client.ObjectKey{Name: "test-del-inject", Namespace: "default"}
		if err := c.Get(ctx, key, svc); err == nil {
			svc.Spec.Ports[0].Port = 8080
			c.Update(ctx, svc)
		}
	}()

	obj := Obj[corev1.Service]("test-del-inject", "default")
	err := watcher.WaitForDeletion(obj)
	if !errors.Is(err, testError) {
		t.Errorf("Expected testError, got: %v", err)
	}
}

// TestErrorInjection_AddEventHandler tests injected error in watchResource.
func TestErrorInjection_AddEventHandler(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)

	watcher := &ResourceWatcher{
		t:            t,
		timeout:      5 * time.Second,
		watchedKinds: make(map[string]any),
		events:       []ResourceEvent{},
		eventCh:      make(chan ResourceEvent, 1000),
	}

	// Inject error
	watcher.testInjectAddEventHandlerError = func() error {
		return testError
	}

	err := watcher.watchResource(ctx, mgr, &corev1.ConfigMap{})
	if !errors.Is(err, testError) {
		t.Errorf("Expected testError, got: %v", err)
	}
}

// TestExtractKind_VerboseFallback tests the impossible fallback path with verbose tag.
func TestExtractKind_VerboseFallback(t *testing.T) {
	// Test helper with NO dot (fallback path)
	result := testExtractKindWithString("SimpleType")
	if result != "SimpleType" {
		t.Errorf("Expected SimpleType, got: %s", result)
	}

	// Test helper with pointer and no dot
	result2 := testExtractKindWithString("*PointerType")
	if result2 != "PointerType" {
		t.Errorf("Expected PointerType, got: %s", result2)
	}

	// Test helper WITH dot (normal path)
	result3 := testExtractKindWithString("package.TypeName")
	if result3 != "TypeName" {
		t.Errorf("Expected TypeName, got: %s", result3)
	}

	// Test helper with pointer and dot
	result4 := testExtractKindWithString("*package.PtrType")
	if result4 != "PtrType" {
		t.Errorf("Expected PtrType, got: %s", result4)
	}

	// Test actual extractKind with injected string (no dot)
	testExtractKindTypeString = "NoDotType"
	actualResult := extractKind(&corev1.Service{})
	if actualResult != "NoDotType" {
		t.Errorf("Expected NoDotType, got: %s", actualResult)
	}

	testExtractKindTypeString = "*PtrNoDot"
	actualResult2 := extractKind(&corev1.Service{})
	if actualResult2 != "PtrNoDot" {
		t.Errorf("Expected PtrNoDot, got: %s", actualResult2)
	}
}

// TestNewResourceWatcher_ErrorPaths tests error handling in NewResourceWatcher.
func TestNewResourceWatcher_ErrorPaths(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setupScheme func() *runtime.Scheme
		wantFatal   bool
	}{
		"option error": {
			setupScheme: func() *runtime.Scheme {
				scheme := runtime.NewScheme()
				_ = corev1.AddToScheme(scheme)
				_ = appsv1.AddToScheme(scheme)
				return scheme
			},
			wantFatal: true,
		},
		"service watch error": {
			setupScheme: func() *runtime.Scheme {
				return runtime.NewScheme() // No schemes registered
			},
			wantFatal: true,
		},
		"statefulset watch error": {
			setupScheme: func() *runtime.Scheme {
				scheme := runtime.NewScheme()
				_ = corev1.AddToScheme(scheme) // Only corev1, missing appsv1
				return scheme
			},
			wantFatal: true,
		},
		"extra resource watch error": {
			setupScheme: func() *runtime.Scheme {
				scheme := runtime.NewScheme()
				_ = corev1.AddToScheme(scheme)
				_ = appsv1.AddToScheme(scheme)
				return scheme
			},
			wantFatal: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := tc.setupScheme()
			ctx := t.Context()
			mgr := SetUpEnvtestManager(t, scheme)

			mock := &mockTB{TB: t}

			switch name {
			case "option error":
				errorOpt := func(rw *ResourceWatcher) error {
					return errors.New("test option error")
				}
				_ = NewResourceWatcher(mock, ctx, mgr, errorOpt)
			case "extra resource watch error":
				type UnknownType struct {
					corev1.Pod
				}
				_ = NewResourceWatcher(mock, ctx, mgr, WithExtraResource(&UnknownType{}))
			default:
				_ = NewResourceWatcher(mock, ctx, mgr)
			}

			if mock.fatalCalled != tc.wantFatal {
				t.Errorf("fatalCalled = %v, want %v", mock.fatalCalled, tc.wantFatal)
			}
		})
	}
}

// TestFindLatestEventFor_EdgeCases tests all conditional branches in findLatestEventFor.
func TestFindLatestEventFor_EdgeCases(t *testing.T) {
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
			evt := watcher.findLatestEventFor(tc.setupObj())
			if (evt != nil) != tc.wantFound {
				t.Errorf("findLatestEventFor() found=%v, want=%v", evt != nil, tc.wantFound)
			}
		})
	}
}

// TestWaitForSingleDeletion_SuccessWithUpdates tests deletion success after seeing UPDATED events.
func TestWaitForSingleDeletion_SuccessWithUpdates(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)
	c := mgr.GetClient()

	watcher := NewResourceWatcher(t, ctx, mgr)
	watcher.SetCmpOpts(IgnoreMetaRuntimeFields(), IgnoreServiceRuntimeFields())

	go func() {
		time.Sleep(100 * time.Millisecond)

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-upd-del", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}
		c.Create(ctx, svc)

		time.Sleep(80 * time.Millisecond)

		// Update multiple times
		for i := 0; i < 2; i++ {
			key := client.ObjectKey{Name: "svc-upd-del", Namespace: "default"}
			if err := c.Get(ctx, key, svc); err == nil {
				svc.Spec.Ports[0].Port = int32(81 + i)
				c.Update(ctx, svc)
				time.Sleep(80 * time.Millisecond)
			}
		}

		// Finally delete
		c.Delete(ctx, svc)
	}()

	err := watcher.WaitForDeletion(Obj[corev1.Service]("svc-upd-del", "default"))
	if err != nil {
		t.Errorf("WaitForDeletion() should succeed, got: %v", err)
	}
}

// TestWatchResource_AlreadyWatched tests early return when kind already watched.
func TestWatchResource_AlreadyWatched(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ctx := t.Context()
	mgr := SetUpEnvtestManager(t, scheme)

	watcher := &ResourceWatcher{
		t:            t,
		watchedKinds: make(map[string]any),
	}

	watcher.watchedKinds["Service"] = nil

	err := watcher.watchResource(ctx, mgr, &corev1.Service{})
	if err != nil {
		t.Errorf("watchResource() for already-watched kind should return nil, got: %v", err)
	}
}
