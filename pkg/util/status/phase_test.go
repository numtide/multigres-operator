package status

import (
	"testing"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func TestComputePhase(t *testing.T) {
	tests := []struct {
		name  string
		ready int32
		total int32
		want  multigresv1alpha1.Phase
	}{
		{
			name:  "Zero Total -> Initializing",
			ready: 0,
			total: 0,
			want:  multigresv1alpha1.PhaseInitializing,
		},
		{
			name:  "Ready Equal Total -> Healthy",
			ready: 3,
			total: 3,
			want:  multigresv1alpha1.PhaseHealthy,
		},
		{
			name:  "Ready Less Than Total -> Progressing",
			ready: 2,
			total: 3,
			want:  multigresv1alpha1.PhaseProgressing,
		},
		{
			name:  "Ready Zero (with Positive Total) -> Progressing",
			ready: 0,
			total: 3,
			want:  multigresv1alpha1.PhaseProgressing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputePhase(tt.ready, tt.total); got != tt.want {
				t.Errorf("ComputePhase() = %v, want %v", got, tt.want)
			}
		})
	}
}
