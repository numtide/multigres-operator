package drain_test

import (
	"context"
	"testing"

	"github.com/multigres/multigres/go/pb/clustermetadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/drain"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

func TestUpdateDrainState_NilAnnotations(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	requeue, err := drain.UpdateDrainState(
		context.Background(),
		c,
		pod,
		metadata.DrainStateDraining,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Error("expected requeue")
	}
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateDraining {
		t.Errorf("expected state to be set, got %v", pod.Annotations)
	}
}

func TestIsPrimaryDraining(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	t.Run("returns false for nil primary", func(t *testing.T) {
		t.Parallel()
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		if drain.IsPrimaryDraining(context.Background(), c, shard, nil) {
			t.Error("expected false for nil primary")
		}
	})

	t.Run("returns false for nil primary ID", func(t *testing.T) {
		t.Parallel()
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		primary := &clustermetadata.MultiPooler{}
		if drain.IsPrimaryDraining(context.Background(), c, shard, primary) {
			t.Error("expected false for nil primary ID")
		}
	})

	t.Run("returns false when primary pod not found", func(t *testing.T) {
		t.Parallel()
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "missing-pod"},
		}
		if drain.IsPrimaryDraining(context.Background(), c, shard, primary) {
			t.Error("expected false when pod not found")
		}
	})

	t.Run("returns false when no drain annotation", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-pod",
				Namespace: "default",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		}
		if drain.IsPrimaryDraining(context.Background(), c, shard, primary) {
			t.Error("expected false when no drain annotation")
		}
	})

	t.Run("returns true when drain annotation present", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-pod",
				Namespace: "default",
				Annotations: map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateDraining,
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		}
		if !drain.IsPrimaryDraining(context.Background(), c, shard, primary) {
			t.Error("expected true when drain annotation present")
		}
	})

	t.Run("returns false when drain state is ReadyForDeletion", func(t *testing.T) {
		t.Parallel()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-pod",
				Namespace: "default",
				Annotations: map[string]string{
					metadata.AnnotationDrainState: metadata.DrainStateReadyForDeletion,
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		}
		if drain.IsPrimaryDraining(context.Background(), c, shard, primary) {
			t.Error("expected false when drain state is ReadyForDeletion")
		}
	})
}
