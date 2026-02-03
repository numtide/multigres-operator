package multigrescluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

func (r *MultigresClusterReconciler) updateStatus(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) error {
	oldPhase := cluster.Status.Phase
	cluster.Status.ObservedGeneration = cluster.Generation
	cluster.Status.Cells = make(map[multigresv1alpha1.CellName]multigresv1alpha1.CellStatusSummary)
	cluster.Status.Databases = make(
		map[multigresv1alpha1.DatabaseName]multigresv1alpha1.DatabaseStatusSummary,
	)

	cells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, cells, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list cells for status: %w", err)
	}

	for _, c := range cells.Items {
		ready := false
		for _, cond := range c.Status.Conditions {
			if cond.Type == "Available" && cond.Status == "True" &&
				c.Status.ObservedGeneration == c.Generation {
				ready = true
				break
			}
		}
		cluster.Status.Cells[multigresv1alpha1.CellName(c.Spec.Name)] = multigresv1alpha1.CellStatusSummary{
			Ready:           ready,
			GatewayReplicas: c.Status.GatewayReplicas,
		}
	}

	tgs := &multigresv1alpha1.TableGroupList{}
	if err := r.List(ctx, tgs, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list tablegroups for status: %w", err)
	}

	dbShards := make(map[multigresv1alpha1.DatabaseName]struct {
		Ready int32
		Total int32
	})

	for _, tg := range tgs.Items {
		stat := dbShards[multigresv1alpha1.DatabaseName(tg.Spec.DatabaseName)]
		// If TableGroup is stale, don't count it as providing ready shards?
		// Or simpler: just ensure we don't count ready shards if TG is stale.
		// However, TG.Status.ReadyShards might be stale itself.
		// Strict check:
		if tg.Status.ObservedGeneration == tg.Generation {
			stat.Ready += tg.Status.ReadyShards
		}
		stat.Total += tg.Status.TotalShards
		dbShards[multigresv1alpha1.DatabaseName(tg.Spec.DatabaseName)] = stat

	}

	for dbName, stat := range dbShards {
		cluster.Status.Databases[dbName] = multigresv1alpha1.DatabaseStatusSummary{
			ReadyShards: stat.Ready,
			TotalShards: stat.Total,
		}
	}

	// Calculate Cluster Phase
	var (
		anyDegraded bool
		allHealthy  = true
	)

	for _, c := range cells.Items {
		switch {
		case c.Status.ObservedGeneration != c.Generation:
			allHealthy = false
		case c.Status.Phase == multigresv1alpha1.PhaseDegraded:
			anyDegraded = true
			allHealthy = false
		case c.Status.Phase == multigresv1alpha1.PhaseHealthy:
			// ok
		default:
			allHealthy = false
		}
	}

	for _, tg := range tgs.Items {
		switch {
		case tg.Status.ObservedGeneration != tg.Generation:
			allHealthy = false
		case tg.Status.Phase == multigresv1alpha1.PhaseDegraded:
			anyDegraded = true
			allHealthy = false
		case tg.Status.Phase == multigresv1alpha1.PhaseHealthy:
			// ok
		default:
			allHealthy = false
		}
	}

	switch {
	case anyDegraded:
		cluster.Status.Phase = multigresv1alpha1.PhaseDegraded
		cluster.Status.Message = "Cluster is degraded"
	case allHealthy:
		cluster.Status.Phase = multigresv1alpha1.PhaseHealthy
		cluster.Status.Message = "Ready"
	default:
		cluster.Status.Phase = multigresv1alpha1.PhaseProgressing
		cluster.Status.Message = "Cluster is progressing"
	}

	allCellsReady := true
	for _, c := range cluster.Status.Cells {
		if !c.Ready {
			allCellsReady = false
			break
		}
	}

	statusStr := metav1.ConditionFalse
	if allCellsReady && len(cluster.Status.Cells) > 0 {
		statusStr = metav1.ConditionTrue
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             statusStr,
		Reason:             "AggregatedStatus",
		Message:            "Aggregation of cell availability",
		LastTransitionTime: metav1.Now(),
	})

	// 1. Construct the Patch Object
	patchObj := &multigresv1alpha1.MultigresCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: multigresv1alpha1.GroupVersion.String(),
			Kind:       "MultigresCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Status: cluster.Status,
	}

	// 2. Apply the Patch
	if oldPhase != cluster.Status.Phase {
		r.Recorder.Eventf(
			cluster,
			"Normal",
			"PhaseChange",
			"Transitioned from '%s' to '%s'",
			oldPhase,
			cluster.Status.Phase,
		)
	}

	if err := r.Status().Patch(
		ctx,
		patchObj,
		client.Apply,
		client.FieldOwner("multigres-operator"),
		client.ForceOwnership,
	); err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}

	return nil
}
