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

func TestValidateBackupStorageClassDependency(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	t.Run("no explicit backup class sets true not-specified condition", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

		if err := r.validateBackupStorageClassDependency(t.Context(), shard); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var updated multigresv1alpha1.Shard
		if err := c.Get(t.Context(), client.ObjectKeyFromObject(shard), &updated); err != nil {
			t.Fatalf("failed to read shard: %v", err)
		}
		cond := findCondition(updated.Status.Conditions, conditionStorageClassValid)
		if cond == nil || cond.Status != metav1.ConditionTrue ||
			cond.Reason != storageClassNotSpecifiedReason {
			t.Fatalf("unexpected condition: %#v", cond)
		}
	})

	t.Run("missing backup class returns dependency error and false condition", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
			Spec: multigresv1alpha1.ShardSpec{
				Backup: &multigresv1alpha1.BackupConfig{
					Type: multigresv1alpha1.BackupTypeFilesystem,
					Filesystem: &multigresv1alpha1.FilesystemBackupConfig{
						Storage: multigresv1alpha1.StorageSpec{Class: "missing-sc"},
					},
				},
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

		err := r.validateBackupStorageClassDependency(t.Context(), shard)
		if err == nil || !isMissingStorageClassDependency(err) {
			t.Fatalf("expected missing dependency error, got: %v", err)
		}

		var updated multigresv1alpha1.Shard
		if getErr := c.Get(
			t.Context(),
			client.ObjectKeyFromObject(shard),
			&updated,
		); getErr != nil {
			t.Fatalf("failed to read shard: %v", getErr)
		}
		cond := findCondition(updated.Status.Conditions, conditionStorageClassValid)
		if cond == nil || cond.Status != metav1.ConditionFalse ||
			cond.Reason != storageClassNotFoundReason {
			t.Fatalf("unexpected condition: %#v", cond)
		}
	})
}

func TestValidatePoolStorageClassDependencies(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	t.Run("no explicit pool class sets true not-specified condition", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
			Spec: multigresv1alpha1.ShardSpec{
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"primary": {
						Storage: multigresv1alpha1.StorageSpec{Size: "10Gi"},
					},
				},
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

		if err := r.validatePoolStorageClassDependencies(t.Context(), shard); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var updated multigresv1alpha1.Shard
		if err := c.Get(t.Context(), client.ObjectKeyFromObject(shard), &updated); err != nil {
			t.Fatalf("failed to read shard: %v", err)
		}
		cond := findCondition(updated.Status.Conditions, conditionStorageClassValid)
		if cond == nil || cond.Status != metav1.ConditionTrue ||
			cond.Reason != storageClassNotSpecifiedReason {
			t.Fatalf("unexpected condition: %#v", cond)
		}
	})

	t.Run("all explicit pool classes present sets true found condition", func(t *testing.T) {
		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
			Spec: multigresv1alpha1.ShardSpec{
				Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
					"primary": {
						Storage: multigresv1alpha1.StorageSpec{Size: "10Gi", Class: "fast"},
					},
				},
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}}).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

		if err := r.validatePoolStorageClassDependencies(t.Context(), shard); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var updated multigresv1alpha1.Shard
		if err := c.Get(t.Context(), client.ObjectKeyFromObject(shard), &updated); err != nil {
			t.Fatalf("failed to read shard: %v", err)
		}
		cond := findCondition(updated.Status.Conditions, conditionStorageClassValid)
		if cond == nil || cond.Status != metav1.ConditionTrue ||
			cond.Reason != storageClassFoundReason {
			t.Fatalf("unexpected condition: %#v", cond)
		}
	})

	t.Run(
		"missing explicit pool class returns dependency error and false condition",
		func(t *testing.T) {
			shard := &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
				Spec: multigresv1alpha1.ShardSpec{
					Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
						"primary": {
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
			r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

			err := r.validatePoolStorageClassDependencies(t.Context(), shard)
			if err == nil || !isMissingStorageClassDependency(err) {
				t.Fatalf("expected missing dependency error, got: %v", err)
			}

			var updated multigresv1alpha1.Shard
			if getErr := c.Get(
				t.Context(),
				client.ObjectKeyFromObject(shard),
				&updated,
			); getErr != nil {
				t.Fatalf("failed to read shard: %v", getErr)
			}
			cond := findCondition(updated.Status.Conditions, conditionStorageClassValid)
			if cond == nil || cond.Status != metav1.ConditionFalse ||
				cond.Reason != storageClassNotFoundReason {
				t.Fatalf("unexpected condition: %#v", cond)
			}
		},
	)
}

func TestReconcile_MissingStorageClassReturnsDependencyRequeueEvenWhenPVCExists(t *testing.T) {
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

	existingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BuildPoolDataPVCName(shard, "primary", "zone1", 0),
			Namespace: "default",
			Labels: buildPoolLabelsWithCell(
				shard,
				"primary",
				"zone1",
			),
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shard, existingPVC).
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
		t.Fatalf("requeueAfter = %v, want %v", result.RequeueAfter, storageClassDependencyRequeue)
	}
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

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
