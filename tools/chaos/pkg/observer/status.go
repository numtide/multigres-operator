package observer

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/common"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/report"
)

func (o *Observer) checkCRDStatus(ctx context.Context) {
	o.checkClusterStatus(ctx)
	o.checkShardStatus(ctx)
	o.checkCellStatus(ctx)
	o.checkTopoServerStatus(ctx)
}

func (o *Observer) checkClusterStatus(ctx context.Context) {
	var clusters multigresv1alpha1.MultigresClusterList
	if err := o.client.List(ctx, &clusters, o.listOpts()...); err != nil {
		return
	}

	for i := range clusters.Items {
		cluster := &clusters.Items[i]
		comp := fmt.Sprintf("cluster/%s/%s", cluster.Namespace, cluster.Name)
		o.checkPhase(comp, cluster.Status.Phase, cluster.DeletionTimestamp != nil)
		o.checkGenerationStaleness(comp, cluster.Generation, cluster.Status.ObservedGeneration)
	}
}

func (o *Observer) checkShardStatus(ctx context.Context) {
	var shards multigresv1alpha1.ShardList
	if err := o.client.List(ctx, &shards, o.listOpts()...); err != nil {
		return
	}

	for i := range shards.Items {
		shard := &shards.Items[i]
		comp := fmt.Sprintf("shard/%s/%s", shard.Namespace, shard.Name)

		o.checkPhase(comp, shard.Status.Phase, shard.DeletionTimestamp != nil)
		o.checkGenerationStaleness(comp, shard.Generation, shard.Status.ObservedGeneration)
		o.checkShardPodRoles(ctx, shard)
		o.checkShardReadiness(ctx, shard)
		o.checkShardCellsList(shard)
		o.checkShardConditions(shard, comp)
	}
}

func (o *Observer) checkCellStatus(ctx context.Context) {
	var cells multigresv1alpha1.CellList
	if err := o.client.List(ctx, &cells, o.listOpts()...); err != nil {
		return
	}

	for i := range cells.Items {
		cell := &cells.Items[i]
		comp := fmt.Sprintf("cell/%s/%s", cell.Namespace, cell.Name)
		o.checkPhase(comp, cell.Status.Phase, cell.DeletionTimestamp != nil)
		o.checkGenerationStaleness(comp, cell.Generation, cell.Status.ObservedGeneration)
	}
}

func (o *Observer) checkTopoServerStatus(ctx context.Context) {
	var topos multigresv1alpha1.TopoServerList
	if err := o.client.List(ctx, &topos, o.listOpts()...); err != nil {
		return
	}

	for i := range topos.Items {
		ts := &topos.Items[i]
		comp := fmt.Sprintf("toposerver/%s/%s", ts.Namespace, ts.Name)
		o.checkPhase(comp, ts.Status.Phase, ts.DeletionTimestamp != nil)
		o.checkGenerationStaleness(comp, ts.Generation, ts.Status.ObservedGeneration)
	}
}

func (o *Observer) checkPhase(component string, phase multigresv1alpha1.Phase, deleting bool) {
	if deleting {
		return
	}

	now := time.Now()
	key := component

	switch phase {
	case multigresv1alpha1.PhaseHealthy:
		delete(o.generationDivergeSince, key+"/phase")
		return
	case multigresv1alpha1.PhaseDegraded, multigresv1alpha1.PhaseUnknown:
		since, tracked := o.generationDivergeSince[key+"/phase"]
		if !tracked {
			o.generationDivergeSince[key+"/phase"] = now
			return
		}
		if now.Sub(since) > common.PhaseDegradedTimeout {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityError,
				Check:     "crd-status",
				Component: component,
				Message:   fmt.Sprintf("%s in %s phase for %s", component, phase, now.Sub(since).Round(time.Second)),
				Details: map[string]any{
					"phase":    string(phase),
					"duration": now.Sub(since).String(),
				},
			})
		}
	case multigresv1alpha1.PhaseInitializing, multigresv1alpha1.PhaseProgressing:
		// These are expected transitional phases, don't alert.
	}
}

func (o *Observer) checkGenerationStaleness(component string, generation, observed int64) {
	now := time.Now()
	key := component + "/gen"

	if observed == generation {
		delete(o.generationDivergeSince, key)
		return
	}

	since, tracked := o.generationDivergeSince[key]
	if !tracked {
		o.generationDivergeSince[key] = now
		return
	}

	if now.Sub(since) > common.GenerationDivergeTimeout {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "crd-status",
			Component: component,
			Message:   fmt.Sprintf("%s observedGeneration (%d) behind metadata.generation (%d) for %s", component, observed, generation, now.Sub(since).Round(time.Second)),
			Details: map[string]any{
				"observedGeneration": observed,
				"generation":         generation,
			},
		})
	}
}

func (o *Observer) checkShardPodRoles(ctx context.Context, shard *multigresv1alpha1.Shard) {
	if shard.DeletionTimestamp != nil {
		return
	}
	comp := fmt.Sprintf("shard/%s/%s", shard.Namespace, shard.Name)
	now := time.Now()

	// Pod labels use the short shard name from the label, not the CRD name.
	shardLabelValue := shard.Labels[common.LabelMultigresShard]

	// Check for stale podRoles entries (pods that no longer exist).
	if shard.Status.PodRoles != nil {
		var existingPods map[string]bool
		var pods corev1.PodList
		if err := o.client.List(ctx, &pods,
			o.listOpts(client.MatchingLabels{common.LabelMultigresShard: shardLabelValue})...,
		); err == nil {
			existingPods = make(map[string]bool, len(pods.Items))
			for i := range pods.Items {
				existingPods[pods.Items[i].Name] = true
			}

			for podName := range shard.Status.PodRoles {
				if !existingPods[podName] {
					staleKey := comp + "/stale-role/" + podName
					since, tracked := o.generationDivergeSince[staleKey]
					if !tracked {
						o.generationDivergeSince[staleKey] = now
						continue
					}
					if now.Sub(since) > common.StaleStatusEntryGracePeriod {
						o.reporter.Report(report.Finding{
							Severity:  report.SeverityWarn,
							Check:     "crd-status",
							Component: comp,
							Message:   fmt.Sprintf("Stale podRoles entry for non-existent pod %s", podName),
							Details:   map[string]any{"pod": podName, "role": shard.Status.PodRoles[podName]},
						})
					}
				}
			}
		}
	}

	// Check primary count per pool per cell.
	// We need actual pod labels to correlate podRoles entries with pool+cell.
	var poolPods corev1.PodList
	if err := o.client.List(ctx, &poolPods,
		o.listOpts(client.MatchingLabels{
			common.LabelMultigresShard: shardLabelValue,
			common.LabelAppComponent:   common.ComponentPool,
		})...,
	); err != nil {
		return
	}

	// Build a lookup from pod name to its pool+cell labels.
	podLabels := make(map[string]podPoolCell, len(poolPods.Items))
	for i := range poolPods.Items {
		p := &poolPods.Items[i]
		podLabels[p.Name] = podPoolCell{
			pool: p.Labels[common.LabelMultigresPool],
			cell: p.Labels[common.LabelMultigresCell],
		}
	}

	for poolName, poolSpec := range shard.Spec.Pools {
		for _, cellName := range poolSpec.Cells {
			violationKey := fmt.Sprintf("%s/%s-%s", comp, poolName, cellName)
			primaryCount := countPrimariesForPoolCell(shard.Status.PodRoles, podLabels, string(poolName), string(cellName))

			if primaryCount != 1 {
				since, tracked := o.primaryViolationSince[violationKey]
				if !tracked {
					o.primaryViolationSince[violationKey] = now
					continue
				}
				if now.Sub(since) > common.PrimaryGracePeriod {
					o.reporter.Report(report.Finding{
						Severity:  report.SeverityError,
						Check:     "crd-status",
						Component: comp,
						Message:   fmt.Sprintf("Pool %s cell %s has %d primaries (expected 1)", poolName, cellName, primaryCount),
						Details: map[string]any{
							"pool":         string(poolName),
							"cell":         string(cellName),
							"primaryCount": primaryCount,
						},
					})
				}
			} else {
				delete(o.primaryViolationSince, violationKey)
			}
		}
	}
}

// podPoolCell holds the pool and cell labels for a pod.
type podPoolCell struct {
	pool string
	cell string
}

// countPrimariesForPoolCell counts pods with a "primary" role that belong to
// the specified pool and cell, using pod labels for accurate filtering.
func countPrimariesForPoolCell(podRoles map[string]string, podLabels map[string]podPoolCell, poolName, cellName string) int {
	count := 0
	for podName, role := range podRoles {
		if role != "primary" && role != "PRIMARY" {
			continue
		}
		info, ok := podLabels[podName]
		if !ok {
			continue
		}
		if info.pool == poolName && info.cell == cellName {
			count++
		}
	}
	return count
}

func (o *Observer) checkShardReadiness(ctx context.Context, shard *multigresv1alpha1.Shard) {
	if shard.DeletionTimestamp != nil {
		return
	}
	comp := fmt.Sprintf("shard/%s/%s", shard.Namespace, shard.Name)
	shardLabelValue := shard.Labels[common.LabelMultigresShard]

	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		o.listOpts(client.MatchingLabels{
			common.LabelMultigresShard: shardLabelValue,
			common.LabelAppComponent:   common.ComponentPool,
		})...,
	); err != nil {
		return
	}

	actualReady := int32(0)
	for i := range pods.Items {
		for _, cs := range pods.Items[i].Status.ContainerStatuses {
			if cs.Ready {
				actualReady++
				break
			}
		}
	}

	if shard.Status.ReadyReplicas != actualReady {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityWarn,
			Check:     "crd-status",
			Component: comp,
			Message:   fmt.Sprintf("status.readyReplicas (%d) != actual ready pods (%d)", shard.Status.ReadyReplicas, actualReady),
		})
	}
}

func (o *Observer) checkShardCellsList(shard *multigresv1alpha1.Shard) {
	if shard.DeletionTimestamp != nil {
		return
	}
	comp := fmt.Sprintf("shard/%s/%s", shard.Namespace, shard.Name)

	// Collect expected cells from pool specs.
	expectedCells := make(map[string]bool)
	for _, pool := range shard.Spec.Pools {
		for _, cell := range pool.Cells {
			expectedCells[string(cell)] = true
		}
	}

	statusCells := make(map[string]bool)
	for _, cell := range shard.Status.Cells {
		statusCells[string(cell)] = true
	}

	for cell := range expectedCells {
		if !statusCells[cell] {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityWarn,
				Check:     "crd-status",
				Component: comp,
				Message:   fmt.Sprintf("Cell %s expected in status.cells but not found", cell),
			})
		}
	}

	for cell := range statusCells {
		if !expectedCells[cell] {
			o.reporter.Report(report.Finding{
				Severity:  report.SeverityWarn,
				Check:     "crd-status",
				Component: comp,
				Message:   fmt.Sprintf("Cell %s in status.cells but not in any pool spec", cell),
			})
		}
	}
}

func (o *Observer) checkShardConditions(shard *multigresv1alpha1.Shard, comp string) {
	for _, cond := range shard.Status.Conditions {
		switch cond.Type {
		case multigresv1alpha1.ConditionReadyForDeletion:
			if shard.DeletionTimestamp == nil && cond.Status == "True" {
				o.reporter.Report(report.Finding{
					Severity:  report.SeverityWarn,
					Check:     "crd-status",
					Component: comp,
					Message:   fmt.Sprintf("Shard %s has ReadyForDeletion=True but is not being deleted", shard.Name),
				})
			}
		case "BackupHealthy":
			if cond.Status == "False" {
				sev := report.SeverityWarn
				if cond.Reason == "BackupFailed" {
					sev = report.SeverityError
				}
				o.reporter.Report(report.Finding{
					Severity:  sev,
					Check:     "crd-status",
					Component: comp,
					Message:   fmt.Sprintf("Shard %s backup unhealthy (%s): %s", shard.Name, cond.Reason, cond.Message),
					Details:   map[string]any{"reason": cond.Reason},
				})
			}
		}
	}
}
