package shard

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestBuildConditions(t *testing.T) {
	tests := map[string]struct {
		shard     *multigresv1alpha1.Shard
		totalPods int32
		readyPods int32
		want      []metav1.Condition
	}{
		"all pods ready": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 5,
				},
			},
			totalPods: 3,
			readyPods: 3,
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
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
			},
			totalPods: 5,
			readyPods: 2,
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
		"no pods ready": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
			},
			totalPods: 4,
			readyPods: 0,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionFalse,
					Reason:             "NotAllPodsReady",
					Message:            "0/4 pods ready",
					ObservedGeneration: 1,
				},
			},
		},
		"zero pods total": {
			shard: &multigresv1alpha1.Shard{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 2,
				},
			},
			totalPods: 0,
			readyPods: 0,
			want: []metav1.Condition{
				{
					Type:               "Available",
					Status:             metav1.ConditionFalse,
					Reason:             "NotAllPodsReady",
					Message:            "0/0 pods ready",
					ObservedGeneration: 2,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			r := &ShardReconciler{}
			got := r.buildConditions(tc.shard, tc.totalPods, tc.readyPods)

			if len(got) != len(tc.want) {
				t.Errorf("buildConditions() returned %d conditions, want %d", len(got), len(tc.want))
				return
			}

			for i, wantCond := range tc.want {
				gotCond := got[i]
				if gotCond.Type != wantCond.Type {
					t.Errorf("condition[%d].Type = %s, want %s", i, gotCond.Type, wantCond.Type)
				}
				if gotCond.Status != wantCond.Status {
					t.Errorf("condition[%d].Status = %s, want %s", i, gotCond.Status, wantCond.Status)
				}
				if gotCond.Reason != wantCond.Reason {
					t.Errorf("condition[%d].Reason = %s, want %s", i, gotCond.Reason, wantCond.Reason)
				}
				if gotCond.Message != wantCond.Message {
					t.Errorf("condition[%d].Message = %s, want %s", i, gotCond.Message, wantCond.Message)
				}
				if gotCond.ObservedGeneration != wantCond.ObservedGeneration {
					t.Errorf("condition[%d].ObservedGeneration = %d, want %d", i, gotCond.ObservedGeneration, wantCond.ObservedGeneration)
				}
			}
		})
	}
}
