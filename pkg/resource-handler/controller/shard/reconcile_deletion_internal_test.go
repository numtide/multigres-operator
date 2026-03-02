package shard

import (
"context"
"testing"

appsv1 "k8s.io/api/apps/v1"
corev1 "k8s.io/api/core/v1"
policyv1 "k8s.io/api/policy/v1"
"k8s.io/apimachinery/pkg/api/errors"
metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
"k8s.io/apimachinery/pkg/runtime"
"k8s.io/apimachinery/pkg/types"
"k8s.io/client-go/tools/record"
"sigs.k8s.io/controller-runtime/pkg/client"
"sigs.k8s.io/controller-runtime/pkg/client/fake"

multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
"github.com/numtide/multigres-operator/pkg/testutil"
"github.com/numtide/multigres-operator/pkg/util/metadata"
)

func TestHandleDeletion_ErrorPaths(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)

	shardLabels := map[string]string{
		metadata.LabelMultigresCluster:    "test-cluster",
		metadata.LabelMultigresDatabase:   "testdb",
		metadata.LabelMultigresTableGroup: "default",
		metadata.LabelMultigresShard:      "shard0",
	}

	baseShard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-shard",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
			Finalizers:        []string{"kubernetes.io/test"},
			Labels:            shardLabels,
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:   "testdb",
			TableGroupName: "default",
			ShardName:      "shard0",
		},
	}

	t.Run("error listing pods", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shard).Build()
		c := testutil.NewFakeClientWithFailures(base, &testutil.FailureConfig{
			OnList: func(list client.ObjectList) error {
				if _, ok := list.(*corev1.PodList); ok {
					return testutil.ErrNetworkTimeout
				}
				return nil
			},
		})
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err == nil {
			t.Error("expected error on Pod list failure")
		}
	})

	t.Run("error listing deployments", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shard).Build()
		c := testutil.NewFakeClientWithFailures(base, &testutil.FailureConfig{
			OnList: func(list client.ObjectList) error {
				if _, ok := list.(*appsv1.DeploymentList); ok {
					return testutil.ErrNetworkTimeout
				}
				return nil
			},
		})
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err == nil {
			t.Error("expected error on Deployment list failure")
		}
	})

	t.Run("error listing PVCs", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shard).Build()
		c := testutil.NewFakeClientWithFailures(base, &testutil.FailureConfig{
			OnList: func(list client.ObjectList) error {
				if _, ok := list.(*corev1.PersistentVolumeClaimList); ok {
					return testutil.ErrNetworkTimeout
				}
				return nil
			},
		})
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err == nil {
			t.Error("expected error on PVC list failure")
		}
	})

	t.Run("error deleting deployment", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deploy",
				Namespace: "default",
				Labels:    shardLabels,
			},
		}
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shard, deploy).Build()
		c := testutil.NewFakeClientWithFailures(base, &testutil.FailureConfig{
			OnDelete: func(obj client.Object) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return testutil.ErrNetworkTimeout
				}
				return nil
			},
		})
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err == nil {
			t.Error("expected error on Deployment delete failure")
		}
	})

	t.Run("WhenDeleted Delete policy deletes PVCs", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		shard.Spec.PVCDeletionPolicy = &multigresv1alpha1.PVCDeletionPolicy{
			WhenDeleted: multigresv1alpha1.DeletePVCRetentionPolicy,
		}
		shard.Spec.Pools = map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
			"primary": {
				Cells: []multigresv1alpha1.CellName{"zone1"},
			},
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-pvc-0",
				Namespace: "default",
				Labels: map[string]string{
					metadata.LabelMultigresCluster:    "test-cluster",
					metadata.LabelMultigresDatabase:   "testdb",
					metadata.LabelMultigresTableGroup: "default",
					metadata.LabelMultigresShard:      "shard0",
					metadata.LabelMultigresPool:       "primary",
				},
			},
		}

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, pvc).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err != nil {
			t.Fatalf("handleDeletion returned error: %v", err)
		}

		// PVC should be deleted
		got := &corev1.PersistentVolumeClaim{}
		err = c.Get(
			context.Background(),
			types.NamespacedName{Name: "data-pvc-0", Namespace: "default"},
			got,
		)
		if !errors.IsNotFound(err) {
			t.Errorf("PVC should have been deleted, but Get returned: %v", err)
		}
	})

	t.Run("WhenDeleted Retain policy keeps PVCs", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		shard.Spec.PVCDeletionPolicy = &multigresv1alpha1.PVCDeletionPolicy{
			WhenDeleted: multigresv1alpha1.RetainPVCRetentionPolicy,
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-pvc-retain",
				Namespace: "default",
				Labels: map[string]string{
					metadata.LabelMultigresCluster:    "test-cluster",
					metadata.LabelMultigresDatabase:   "testdb",
					metadata.LabelMultigresTableGroup: "default",
					metadata.LabelMultigresShard:      "shard0",
				},
			},
		}

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, pvc).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err != nil {
			t.Fatalf("handleDeletion returned error: %v", err)
		}

		got := &corev1.PersistentVolumeClaim{}
		if err := c.Get(
			context.Background(),
			types.NamespacedName{Name: "data-pvc-retain", Namespace: "default"},
			got,
		); err != nil {
			t.Errorf("PVC should have been retained, but Get returned: %v", err)
		}
	})

	t.Run("error deleting PVC with Delete policy", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		shard.Spec.PVCDeletionPolicy = &multigresv1alpha1.PVCDeletionPolicy{
			WhenDeleted: multigresv1alpha1.DeletePVCRetentionPolicy,
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-pvc-fail",
				Namespace: "default",
				Labels:    shardLabels,
			},
		}

		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shard, pvc).Build()
		c := testutil.NewFakeClientWithFailures(base, &testutil.FailureConfig{
			OnDelete: func(obj client.Object) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					return testutil.ErrNetworkTimeout
				}
				return nil
			},
		})
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err == nil {
			t.Error("expected error on PVC delete failure")
		}
	})

	t.Run("error deleting pod during cleanup", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-cleanup",
				Namespace: "default",
				Labels:    shardLabels,
			},
		}

		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shard, pod).Build()
		c := testutil.NewFakeClientWithFailures(base, &testutil.FailureConfig{
			OnDelete: func(obj client.Object) error {
				if p, ok := obj.(*corev1.Pod); ok && p.Name == "pod-cleanup" {
					return testutil.ErrNetworkTimeout
				}
				return nil
			},
		})
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err == nil {
			t.Error("expected error on pod delete failure")
		}
	})

	t.Run("pool-level PVC policy overrides shard-level", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		shard.Spec.PVCDeletionPolicy = &multigresv1alpha1.PVCDeletionPolicy{
			WhenDeleted: multigresv1alpha1.RetainPVCRetentionPolicy,
		}
		shard.Spec.Pools = map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
			"primary": {
				Cells: []multigresv1alpha1.CellName{"zone1"},
				PVCDeletionPolicy: &multigresv1alpha1.PVCDeletionPolicy{
					WhenDeleted: multigresv1alpha1.DeletePVCRetentionPolicy,
				},
			},
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pool-pvc-override",
				Namespace: "default",
				Labels: map[string]string{
					metadata.LabelMultigresCluster:    "test-cluster",
					metadata.LabelMultigresDatabase:   "testdb",
					metadata.LabelMultigresTableGroup: "default",
					metadata.LabelMultigresShard:      "shard0",
					metadata.LabelMultigresPool:       "primary",
				},
			},
		}

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, pvc).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err != nil {
			t.Fatalf("handleDeletion returned error: %v", err)
		}

		got := &corev1.PersistentVolumeClaim{}
		err = c.Get(
			context.Background(),
			types.NamespacedName{Name: "pool-pvc-override", Namespace: "default"},
			got,
		)
		if !errors.IsNotFound(err) {
			t.Errorf(
				"PVC should have been deleted per pool-level policy override, but Get returned: %v",
				err,
			)
		}
	})

	t.Run("shared backup PVC uses shard-level policy when no pool label", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		shard.Spec.PVCDeletionPolicy = &multigresv1alpha1.PVCDeletionPolicy{
			WhenDeleted: multigresv1alpha1.DeletePVCRetentionPolicy,
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backup-pvc-shared",
				Namespace: "default",
				Labels: map[string]string{
					metadata.LabelMultigresCluster:    "test-cluster",
					metadata.LabelMultigresDatabase:   "testdb",
					metadata.LabelMultigresTableGroup: "default",
					metadata.LabelMultigresShard:      "shard0",
					// no pool label => shared backup PVC
				},
			},
		}

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, pvc).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err != nil {
			t.Fatalf("handleDeletion returned error: %v", err)
		}

		got := &corev1.PersistentVolumeClaim{}
		err = c.Get(
			context.Background(),
			types.NamespacedName{Name: "backup-pvc-shared", Namespace: "default"},
			got,
		)
		if !errors.IsNotFound(err) {
			t.Errorf("shared backup PVC should have been deleted, but Get returned: %v", err)
		}
	})

	t.Run("pool PVC with unknown pool name falls back to shard policy", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		shard.Spec.PVCDeletionPolicy = &multigresv1alpha1.PVCDeletionPolicy{
			WhenDeleted: multigresv1alpha1.DeletePVCRetentionPolicy,
		}
		shard.Spec.Pools = map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
			"primary": {Cells: []multigresv1alpha1.CellName{"zone1"}},
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "orphan-pool-pvc",
				Namespace: "default",
				Labels: map[string]string{
					metadata.LabelMultigresCluster:    "test-cluster",
					metadata.LabelMultigresDatabase:   "testdb",
					metadata.LabelMultigresTableGroup: "default",
					metadata.LabelMultigresShard:      "shard0",
					metadata.LabelMultigresPool:       "removed-pool",
				},
			},
		}

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, pvc).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err != nil {
			t.Fatalf("handleDeletion returned error: %v", err)
		}

		got := &corev1.PersistentVolumeClaim{}
		err = c.Get(
			context.Background(),
			types.NamespacedName{Name: "orphan-pool-pvc", Namespace: "default"},
			got,
		)
		if !errors.IsNotFound(err) {
			t.Errorf(
				"PVC for removed pool should be deleted per shard policy, but Get returned: %v",
				err,
			)
		}
	})

	t.Run("deletion with existing deployments deletes them", func(t *testing.T) {
		shard := baseShard.DeepCopy()
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mo-deploy",
				Namespace: "default",
				Labels:    shardLabels,
			},
		}

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(shard, deploy).
			WithStatusSubresource(&multigresv1alpha1.Shard{}).
			Build()
		r := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), CreateTopoStore: newMemoryTopoFactory()}

		_, err := r.handleDeletion(context.Background(), shard)
		if err != nil {
			t.Fatalf("handleDeletion returned error: %v", err)
		}

		got := &appsv1.Deployment{}
		err = c.Get(
			context.Background(),
			types.NamespacedName{Name: "mo-deploy", Namespace: "default"},
			got,
		)
		if !errors.IsNotFound(err) {
			t.Errorf("deployment should have been deleted, but Get returned: %v", err)
		}
	})
}
