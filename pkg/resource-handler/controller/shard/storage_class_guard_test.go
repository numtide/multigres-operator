package shard

import (
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres-operator/pkg/util/metadata"
	"k8s.io/client-go/tools/record"
)

func TestEnsureStorageClassExists(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	baseShard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	t.Run("missing storageclass sets false condition and returns dependency error", func(t *testing.T) {
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(baseShard.DeepCopy()).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

		err := r.ensureStorageClassExists(
			t.Context(),
			baseShard.DeepCopy(),
			"missing-sc",
			"pool primary PVC",
		)
		if err == nil {
			t.Fatal("expected missing dependency error")
		}
		if !isMissingStorageClassDependency(err) {
			t.Fatalf("expected missing storageclass dependency error, got: %v", err)
		}

		var updated multigresv1alpha1.Shard
		if getErr := c.Get(t.Context(), client.ObjectKeyFromObject(baseShard), &updated); getErr != nil {
			t.Fatalf("failed to get updated shard: %v", getErr)
		}
		cond := metaFindCondition(updated.Status.Conditions, conditionStorageClassValid)
		if cond == nil {
			t.Fatal("expected StorageClassValid condition to be set")
		}
		if cond.Status != metav1.ConditionFalse {
			t.Fatalf("condition status = %s, want False", cond.Status)
		}
	})

	t.Run("existing storageclass sets true condition", func(t *testing.T) {
		sc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{Name: "fast"},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(baseShard.DeepCopy(), sc).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

		if err := r.ensureStorageClassExists(
			t.Context(),
			baseShard.DeepCopy(),
			"fast",
			"pool primary PVC",
		); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var updated multigresv1alpha1.Shard
		if getErr := c.Get(t.Context(), client.ObjectKeyFromObject(baseShard), &updated); getErr != nil {
			t.Fatalf("failed to get updated shard: %v", getErr)
		}
		cond := metaFindCondition(updated.Status.Conditions, conditionStorageClassValid)
		if cond == nil {
			t.Fatal("expected StorageClassValid condition to be set")
		}
		if cond.Status != metav1.ConditionTrue {
			t.Fatalf("condition status = %s, want True", cond.Status)
		}
	})
}

func TestCreateMissingResources_MissingStorageClass(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "s1",
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	_, _, err := r.createMissingResources(
		t.Context(),
		shard,
		"primary",
		"zone1",
		multigresv1alpha1.PoolSpec{
			ReplicasPerCell: ptr.To(int32(1)),
			Cells:           []multigresv1alpha1.CellName{"zone1"},
			Storage: multigresv1alpha1.StorageSpec{
				Size:  "10Gi",
				Class: "missing-sc",
			},
		},
		map[string]*corev1.Pod{},
		map[string]*corev1.PersistentVolumeClaim{},
		1,
	)
	if err == nil {
		t.Fatal("expected missing storageclass dependency error")
	}
	if !isMissingStorageClassDependency(err) {
		t.Fatalf("expected missing storageclass dependency error, got: %v", err)
	}
}

func TestReconcile_MissingStorageClassReturnsDependencyRequeue(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "db",
			TableGroupName: "tg",
			ShardName:      "s1",
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"primary": {
					ReplicasPerCell: ptr.To(int32(1)),
					Cells:           []multigresv1alpha1.CellName{"zone1"},
					Storage: multigresv1alpha1.StorageSpec{
						Size:  "10Gi",
						Class: "missing-sc",
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard).
		WithStatusSubresource(&multigresv1alpha1.Shard{}).
		Build()

	r := &ShardReconciler{
		Client:          c,
		Scheme:          scheme,
		Recorder:        record.NewFakeRecorder(100),
		APIReader:       c,
		CreateTopoStore: newMemoryTopoFactory(),
	}

	result, err := r.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(shard),
	})
	if err != nil {
		t.Fatalf("expected non-error dependency requeue, got error: %v", err)
	}
	if result.RequeueAfter != storageClassDependencyRequeue {
		t.Fatalf(
			"requeueAfter = %v, want %v",
			result.RequeueAfter,
			storageClassDependencyRequeue,
		)
	}

	var updated multigresv1alpha1.Shard
	if getErr := c.Get(t.Context(), client.ObjectKeyFromObject(shard), &updated); getErr != nil {
		t.Fatalf("failed to get updated shard: %v", getErr)
	}
	cond := metaFindCondition(updated.Status.Conditions, conditionStorageClassValid)
	if cond == nil {
		t.Fatal("expected StorageClassValid condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("condition status = %s, want False", cond.Status)
	}
}

func metaFindCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func TestIsMissingStorageClassDependencyWrapped(t *testing.T) {
	err := errors.New("other")
	if isMissingStorageClassDependency(err) {
		t.Fatal("expected false for non-dependency error")
	}

	wrapped := errors.Join(errors.New("outer"), &missingStorageClassDependencyError{className: "x"})
	if !isMissingStorageClassDependency(wrapped) {
		t.Fatal("expected true for wrapped missing dependency error")
	}
}
