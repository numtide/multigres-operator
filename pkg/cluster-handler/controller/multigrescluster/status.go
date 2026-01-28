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
	cluster.Status.ObservedGeneration = cluster.Generation
	cluster.Status.Cells = make(map[multigresv1alpha1.CellName]multigresv1alpha1.CellStatusSummary)
	cluster.Status.Databases = make(map[multigresv1alpha1.DatabaseName]multigresv1alpha1.DatabaseStatusSummary)

	cells := &multigresv1alpha1.CellList{}
	if err := r.List(ctx, cells, client.InNamespace(cluster.Namespace), client.MatchingLabels{"multigres.com/cluster": cluster.Name}); err != nil {
		return fmt.Errorf("failed to list cells for status: %w", err)
	}

	for _, c := range cells.Items {
		ready := false
		for _, cond := range c.Status.Conditions {
			if cond.Type == "Available" && cond.Status == "True" {
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
		stat.Ready += tg.Status.ReadyShards
		stat.Total += tg.Status.TotalShards
		dbShards[multigresv1alpha1.DatabaseName(tg.Spec.DatabaseName)] = stat

	}

	for dbName, stat := range dbShards {
		cluster.Status.Databases[dbName] = multigresv1alpha1.DatabaseStatusSummary{
			ReadyShards: stat.Ready,
			TotalShards: stat.Total,
		}
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

	if err := r.Status().Update(ctx, cluster); err != nil {
		return fmt.Errorf("failed to update cluster status: %w", err)
	}

	return nil
}
