package shard

import (
	"context"
	"fmt"
	"time"

	"github.com/multigres/multigres/go/common/topoclient"
	clustermetadatapb "github.com/multigres/multigres/go/pb/clustermetadata"
	multipoolermanagerdatapb "github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

const (
	// drainTimeout is the maximum time to wait for a failover or drain operation
	// before giving up and reporting an error.
	drainTimeout = 5 * time.Minute
)

// executeDrainStateMachine handles the graceful scale down and etcd unregistration for a pod.
// Returns true if reconciliation should be requeued.
func (r *ShardReconciler) executeDrainStateMachine(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	pod *corev1.Pod,
) (bool, error) {
	logger := log.FromContext(ctx)

	state := pod.Annotations[metadata.AnnotationDrainState]
	if state == "" || state == metadata.DrainStateReadyForDeletion {
		return false, nil
	}

	store, err := r.getTopoStore(shard)
	if err != nil {
		return false, fmt.Errorf("creating topology store: %w", err)
	}
	defer func() { _ = store.Close() }()

	clusterName := shard.Labels[metadata.LabelMultigresCluster]

	// Node Failure Safety: If the pod is stuck terminating for > 5 minutes, force unregister.
	if !pod.DeletionTimestamp.IsZero() && time.Since(pod.DeletionTimestamp.Time) > drainTimeout {
		logger.Info("Pod is stuck terminating, forcing unregistration", "pod", pod.Name)
		if err := r.forceUnregister(ctx, store, pod); err != nil {
			return false, fmt.Errorf("forcing unregistration: %w", err)
		}
		return r.updateDrainState(ctx, pod, metadata.DrainStateReadyForDeletion)
	}

	cells := collectCells(shard)
	cellName := pod.Labels[metadata.LabelMultigresCell]

	// Find the pooler entry for this pod
	var myPooler *topoclient.MultiPoolerInfo
	poolers, err := store.GetMultiPoolersByCell(ctx, cellName, nil)
	if err != nil && !isTopoUnavailable(err) {
		return false, fmt.Errorf("listing poolers in cell %q: %w", cellName, err)
	}

	for _, p := range poolers {
		// In multigres, the pooler ID depends on the hostname which is the pod name
		if fmt.Sprintf("%v", p.Id) == pod.Name ||
			p.GetHostname() == pod.Name { // Fallback heuristics for matching
			myPooler = p
			break
		}
	}

	// We only want to handle valid states
	switch state {
	case metadata.DrainStateRequested:
		if myPooler != nil && myPooler.Type == clustermetadatapb.PoolerType_PRIMARY {
			// It is a primary. We need to initiate a failover first.
			logger.Info("Pod is PRIMARY, triggering failover", "pod", pod.Name)

			// Best effort: find another pooler to become primary
			var otherPooler *topoclient.MultiPoolerInfo
			for _, cell := range cells {
				pp, _ := store.GetMultiPoolersByCell(ctx, cell, nil)
				for _, p := range pp {
					if p.Type != clustermetadatapb.PoolerType_PRIMARY &&
						(fmt.Sprintf("%v", p.Id) != pod.Name && p.GetHostname() != pod.Name) {
						otherPooler = p
						break
					}
				}
				if otherPooler != nil {
					break
				}
			}

			if otherPooler != nil {
				if r.rpcClient == nil {
					logger.Info(
						"RPC client not configured, cannot trigger failover",
						"pod",
						pod.Name,
					)
					return true, nil
				}
				_, err = r.rpcClient.Promote(
					ctx,
					otherPooler.MultiPooler,
					&multipoolermanagerdatapb.PromoteRequest{},
				)
				if err != nil {
					logger.Error(
						err,
						"Failed to appoint new leader",
						"newPrimary",
						otherPooler.GetHostname(),
					)
					return true, nil // Requeue and try again
				}
				r.Recorder.Eventf(
					shard,
					"Warning",
					"FailoverInitiated",
					"Initiated failover from %s to %s",
					pod.Name,
					otherPooler.GetHostname(),
				)
			} else {
				// No replicas available — check if we've exceeded the drain timeout.
				if reqAt, ok := pod.Annotations[metadata.AnnotationDrainRequestedAt]; ok {
					if t, parseErr := time.Parse(
						time.RFC3339,
						reqAt,
					); parseErr == nil &&
						time.Since(t) > drainTimeout {
						r.Recorder.Eventf(
							shard,
							"Warning",
							"FailoverTimeout",
							"Cannot failover PRIMARY pod %s: no eligible replicas found after %s",
							pod.Name,
							drainTimeout,
						)
						monitoring.IncrementDrainOperations(clusterName, shard.Name, "failure")
						return false, fmt.Errorf(
							"failover timeout: no replicas available for PRIMARY pod %s after %s",
							pod.Name,
							drainTimeout,
						)
					}
				}
				logger.Info("No replicas available for failover, will retry", "pod", pod.Name)
			}
			return true, nil // Requeue until it becomes a replica
		}

		// Proceed to draining (call UpdateSynchronousStandbyList REMOVE on primary)
		logger.Info("Proceeding to drain pod", "pod", pod.Name)

		// Get the current primary to remove this replica from synchronous standby
		primary, err := findPrimaryPooler(ctx, store, cells)
		if err == nil && primary != nil && myPooler != nil && r.rpcClient != nil {
			req := &multipoolermanagerdatapb.UpdateSynchronousStandbyListRequest{
				Operation:  multipoolermanagerdatapb.StandbyUpdateOperation_STANDBY_UPDATE_OPERATION_REMOVE,
				StandbyIds: []*clustermetadatapb.ID{myPooler.Id},
			}
			_, rpcErr := r.rpcClient.UpdateSynchronousStandbyList(ctx, primary, req)
			if rpcErr != nil {
				logger.Error(
					rpcErr,
					"Failed to remove pod from synchronous standby list",
					"pod",
					pod.Name,
				)
				return true, nil
			}
		}

		return r.updateDrainState(ctx, pod, metadata.DrainStateDraining)

	case metadata.DrainStateDraining:
		// Verify that the standby removal actually took effect by re-attempting the
		// idempotent REMOVE call on the primary. If the primary is unreachable, requeue.
		primary, err := findPrimaryPooler(ctx, store, cells)
		if err != nil {
			logger.Error(
				err,
				"Failed to find primary for drain verification, will retry",
				"pod",
				pod.Name,
			)
			return true, nil
		}
		if primary != nil && myPooler != nil {
			req := &multipoolermanagerdatapb.UpdateSynchronousStandbyListRequest{
				Operation:  multipoolermanagerdatapb.StandbyUpdateOperation_STANDBY_UPDATE_OPERATION_REMOVE,
				StandbyIds: []*clustermetadatapb.ID{myPooler.Id},
			}
			_, rpcErr := r.rpcClient.UpdateSynchronousStandbyList(ctx, primary, req)
			if rpcErr != nil {
				logger.Error(
					rpcErr,
					"Standby removal verification failed, will retry",
					"pod",
					pod.Name,
				)
				return true, nil
			}
		}
		return r.updateDrainState(ctx, pod, metadata.DrainStateAcknowledged)

	case metadata.DrainStateAcknowledged:
		// Finally, call UnregisterMultiPooler
		if myPooler != nil {
			if err := r.forceUnregister(ctx, store, pod); err != nil {
				return false, fmt.Errorf("unregistering pooler: %w", err)
			}
		}

		monitoring.IncrementDrainOperations(clusterName, shard.Name, "success")
		r.Recorder.Eventf(shard, "Normal", "DrainCompleted", "Pod %s completely drained", pod.Name)
		return r.updateDrainState(ctx, pod, metadata.DrainStateReadyForDeletion)
	}

	return false, nil
}

func (r *ShardReconciler) updateDrainState(
	ctx context.Context,
	pod *corev1.Pod,
	newState string,
) (bool, error) {
	patch := client.MergeFrom(pod.DeepCopy())
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[metadata.AnnotationDrainState] = newState
	if err := r.Patch(ctx, pod, patch); err != nil {
		return false, fmt.Errorf("updating pod drain state to %s: %w", newState, err)
	}
	return true, nil
}

func (r *ShardReconciler) forceUnregister(
	ctx context.Context,
	store topoclient.Store,
	pod *corev1.Pod,
) error {
	cellName := pod.Labels[metadata.LabelMultigresCell]
	if cellName == "" {
		return nil
	}

	poolers, err := store.GetMultiPoolersByCell(ctx, cellName, nil)
	if err != nil {
		return err
	}

	for _, p := range poolers {
		if fmt.Sprintf("%v", p.Id) == pod.Name || p.GetHostname() == pod.Name {
			return store.UnregisterMultiPooler(ctx, p.Id)
		}
	}
	log.FromContext(ctx).
		Info("No matching pooler found in topology for pod, skipping unregistration",
			"pod", pod.Name, "cell", cellName)
	return nil
}
