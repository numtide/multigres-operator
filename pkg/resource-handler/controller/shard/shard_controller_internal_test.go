package shard

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
)

func TestBuildConditions(t *testing.T) {
	tests := map[string]struct {
		generation int64
		totalPods  int32
		readyPods  int32
		want       []metav1.Condition
	}{
		"all pods ready": {
			generation: 5,
			totalPods:  3,
			readyPods:  3,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionTrue,
					Reason:             "AllPodsReady",
					Message:            "All 3 pods are ready",
					ObservedGeneration: 5,
				},
			},
		},
		"partial pods ready": {
			generation: 10,
			totalPods:  5,
			readyPods:  2,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionFalse,
					Reason:             "NotAllPodsReady",
					Message:            "2/5 pods ready",
					ObservedGeneration: 10,
				},
			},
		},
		"no pods": {
			generation: 1,
			totalPods:  0,
			readyPods:  0,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionFalse,
					Reason:             "NotAllPodsReady",
					Message:            "0/0 pods ready",
					ObservedGeneration: 1,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			shard := &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{Generation: tc.generation},
			}
			r := &ShardReconciler{}
			got := r.buildConditions(shard, tc.totalPods, tc.readyPods)

			// Use go-cmp for exact match, ignoring LastTransitionTime
			opts := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
			if diff := cmp.Diff(tc.want, got, opts); diff != "" {
				t.Errorf("buildConditions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
