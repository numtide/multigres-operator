package shard

import (
	"context"
	"fmt"
	"strings"
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
	store topoclient.Store,
	shard *multigresv1alpha1.Shard,
	pod *corev1.Pod,
) (bool, error) {
	logger := log.FromContext(ctx)

	state := pod.Annotations[metadata.AnnotationDrainState]
	if state == "" || state == metadata.DrainStateReadyForDeletion {
		return false, nil
	}

	clusterName := shard.Labels[metadata.LabelMultigresCluster]

	// Node Failure Safety: If the pod is stuck terminating for > 5 minutes, force unregister.
	if !pod.DeletionTimestamp.IsZero() && time.Since(pod.DeletionTimestamp.Time) > drainTimeout {
		logger.Info("Pod is stuck terminating, forcing unregistration", "pod", pod.Name)
		if err := r.forceUnregister(ctx, store, shard, pod); err != nil {
			return false, fmt.Errorf("forcing unregistration: %w", err)
		}
		return r.updateDrainState(ctx, pod, metadata.DrainStateReadyForDeletion)
	}

	cells := collectCells(shard)
	cellName := pod.Labels[metadata.LabelMultigresCell]

	opt := &topoclient.GetMultiPoolersByCellOptions{
		DatabaseShard: &topoclient.DatabaseShard{
			Database:   string(shard.Spec.DatabaseName),
			TableGroup: string(shard.Spec.TableGroupName),
			Shard:      string(shard.Spec.ShardName),
		},
	}

	// Find the pooler entry for this pod
	var myPooler *topoclient.MultiPoolerInfo
	poolers, err := store.GetMultiPoolersByCell(ctx, cellName, opt)
	if err != nil {
		if isTopoUnavailable(err) && !pod.DeletionTimestamp.IsZero() {
			logger.Info(
				"Topology is unavailable while pod is being deleted. Bypassing drain",
				"pod",
				pod.Name,
			)
			return r.updateDrainState(ctx, pod, metadata.DrainStateReadyForDeletion)
		}
		if !isTopoUnavailable(err) {
			return false, fmt.Errorf("listing poolers in cell %q: %w", cellName, err)
		}
	}

	for _, p := range poolers {
		if podMatchesPooler(pod.Name, p) {
			myPooler = p
			break
		}
	}

	isPrimary := myPooler != nil && myPooler.Type == clustermetadatapb.PoolerType_PRIMARY

	// We only want to handle valid states
	switch state {
	case metadata.DrainStateRequested:
		if isPrimary {
			// Failover is multiorch's responsibility via its consensus protocol
			// (BeginTerm + Promote). The operator proceeds with the drain and
			// multiorch will elect a new leader once this pod is removed.
			logger.Info("Draining PRIMARY pod, multiorch will handle failover", "pod", pod.Name)
			r.Recorder.Eventf(
				shard,
				"Warning",
				"PrimaryDrain",
				"Draining PRIMARY pod %s; multiorch will elect a new leader",
				pod.Name,
			)
		} else {
			// Remove this replica from the synchronous standby list on the primary
			// so that quorum calculations no longer include it.
			logger.Info("Proceeding to drain replica pod", "pod", pod.Name)
			primary, err := findPrimaryPooler(ctx, store, shard, cells)
			if err == nil && primary != nil && myPooler != nil && r.rpcClient != nil {
				if r.isPrimaryTerminatingOrMissing(ctx, shard, primary) {
					logger.Info(
						"Primary pod is dead or terminating, skipping standby removal",
						"pod",
						pod.Name,
					)
				} else if r.isPrimaryDraining(ctx, shard, primary) {
					logger.Info(
						"Primary pod is being drained, delaying standby removal",
						"pod",
						pod.Name,
					)
					return true, nil
				} else {
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
			}
		}

		return r.updateDrainState(ctx, pod, metadata.DrainStateDraining)

	case metadata.DrainStateDraining:
		if !isPrimary {
			// Verify that the standby removal actually took effect by re-attempting
			// the idempotent REMOVE call on the primary.
			primary, err := findPrimaryPooler(ctx, store, shard, cells)
			if err != nil {
				logger.Error(
					err,
					"Failed to find primary for drain verification, will retry",
					"pod",
					pod.Name,
				)
				return true, nil
			}
			if primary != nil && myPooler != nil && r.rpcClient != nil {
				if r.isPrimaryTerminatingOrMissing(ctx, shard, primary) {
					logger.Info(
						"Primary pod is dead or terminating, skipping standby removal verification",
						"pod",
						pod.Name,
					)
				} else if r.isPrimaryDraining(ctx, shard, primary) {
					logger.Info(
						"Primary pod is being drained, delaying standby removal verification",
						"pod",
						pod.Name,
					)
					return true, nil
				} else {
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
			}
		}
		return r.updateDrainState(ctx, pod, metadata.DrainStateAcknowledged)

	case metadata.DrainStateAcknowledged:
		// Finally, call UnregisterMultiPooler
		if myPooler != nil {
			if err := r.forceUnregister(ctx, store, shard, pod); err != nil {
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
	shard *multigresv1alpha1.Shard,
	pod *corev1.Pod,
) error {
	cellName := pod.Labels[metadata.LabelMultigresCell]
	if cellName == "" {
		return nil
	}

	opt := &topoclient.GetMultiPoolersByCellOptions{
		DatabaseShard: &topoclient.DatabaseShard{
			Database:   string(shard.Spec.DatabaseName),
			TableGroup: string(shard.Spec.TableGroupName),
			Shard:      string(shard.Spec.ShardName),
		},
	}
	poolers, err := store.GetMultiPoolersByCell(ctx, cellName, opt)
	if err != nil {
		return err
	}

	for _, p := range poolers {
		if podMatchesPooler(pod.Name, p) {
			return store.UnregisterMultiPooler(ctx, p.Id)
		}
	}
	log.FromContext(ctx).
		Info("No matching pooler found in topology for pod, skipping unregistration",
			"pod", pod.Name, "cell", cellName)
	return nil
}

// isPrimaryTerminatingOrMissing checks if the primary pooler's corresponding Kubernetes pod
// is unavailable for receiving RPCs because it is either missing or terminating.
func (r *ShardReconciler) isPrimaryTerminatingOrMissing(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	primary *clustermetadatapb.MultiPooler,
) bool {
	if primary == nil || primary.Id == nil {
		return true
	}
	primaryPod := &corev1.Pod{}
	key := client.ObjectKey{Namespace: shard.Namespace, Name: primary.Id.Name}
	if err := r.Get(ctx, key, primaryPod); err != nil {
		// If the pod doesn't exist or we can't get it, it's unavailable for RPC.
		return true
	}
	// If the pod is terminating, it's shutting down and unavailable for RPC.
	return !primaryPod.DeletionTimestamp.IsZero()
}

// isPrimaryDraining checks if the primary pooler's corresponding Kubernetes pod
// has drain annotations, indicating it is mid-drain and should not receive RPCs.
func (r *ShardReconciler) isPrimaryDraining(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	primary *clustermetadatapb.MultiPooler,
) bool {
	if primary == nil || primary.Id == nil {
		return false
	}
	primaryPod := &corev1.Pod{}
	key := client.ObjectKey{Namespace: shard.Namespace, Name: primary.Id.Name}
	if err := r.Get(ctx, key, primaryPod); err != nil {
		return false
	}
	return primaryPod.Annotations[metadata.AnnotationDrainState] != ""
}

// podMatchesPooler checks if the topology pooler record corresponds to the given Kubernetes pod name.
func podMatchesPooler(podName string, p *topoclient.MultiPoolerInfo) bool {
	if p.Id != nil && p.Id.Name == podName {
		return true
	}
	h := p.GetHostname()
	return h == podName || strings.HasPrefix(h, podName+".")
}
