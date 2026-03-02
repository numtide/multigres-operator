package shard

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/multigres/multigres/go/common/rpcclient"
	"github.com/multigres/multigres/go/common/topoclient"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"k8s.io/client-go/tools/record"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/backuphealth"
	"github.com/numtide/multigres-operator/pkg/data-handler/drain"
	"github.com/numtide/multigres-operator/pkg/data-handler/topo"
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/status"
)

const (
	finalizerName = "multigres.com/shard-data-protection"

	// conditionDatabaseRegistered tracks whether the database was ever
	// successfully registered in the global topology. Used during deletion
	// to distinguish "never initialized" from "temporarily unreachable".
	conditionDatabaseRegistered = "DatabaseRegistered"

	// topoUnavailableGracePeriod is the duration after resource creation during
	// which topology UNAVAILABLE errors are silently requeued instead of being
	// reported as reconcile errors. This prevents noisy error metrics during
	// normal cluster startup while the toposerver is still initializing.
	topoUnavailableGracePeriod = 2 * time.Minute

	// topoUnavailableRequeueDelay is the delay before retrying when the topology
	// server is unavailable during the grace period.
	topoUnavailableRequeueDelay = 5 * time.Second

	// topoCleanupTimeout is the maximum duration to retry topology cleanup
	// during deletion when the database was previously registered but is now
	// unreachable. After this period, the finalizer is removed with a warning.
	topoCleanupTimeout = 2 * time.Minute
)

// ShardReconciler reconciles Shard data plane operations.
// This controller is responsible for:
// - Registering databases in the etcd topology based on shards
// - Managing database metadata in the global topology
// - Cleaning up database data from topology on deletion
//
// The database name comes from Shard.Spec.DatabaseName
// The cells come from the pools defined in the shard
type ShardReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	createTopoStore func(*multigresv1alpha1.Shard) (topoclient.Store, error)
	rpcClient       rpcclient.MultiPoolerClient
}

// Reconcile handles Shard resource reconciliation for data plane operations.
func (r *ShardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	ctx, span := monitoring.StartReconcileSpan(
		ctx,
		"ShardData.Reconcile",
		req.Name,
		req.Namespace,
		"Shard",
	)
	defer span.End()
	ctx = monitoring.EnrichLoggerWithTrace(ctx)

	logger := log.FromContext(ctx)
	logger.V(1).Info("reconcile started")

	// Fetch the Shard instance
	shard := &multigresv1alpha1.Shard{}
	if err := r.Get(ctx, req.NamespacedName, shard); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Shard resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		monitoring.RecordSpanError(span, err)
		logger.Error(err, "Failed to get Shard")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !shard.DeletionTimestamp.IsZero() {
		_, childSpan := monitoring.StartChildSpan(ctx, "ShardData.HandleDeletion")
		result, err := r.handleDeletion(ctx, shard)
		if err != nil {
			monitoring.RecordSpanError(childSpan, err)
		}
		childSpan.End()
		return result, err
	}

	// Add finalizer if not present
	if !slices.Contains(shard.Finalizers, finalizerName) {
		shard.Finalizers = append(shard.Finalizers, finalizerName)
		if err := r.Update(ctx, shard); err != nil {
			if apierrors.IsConflict(err) {
				// Requeue on conflict without emitting a warning event.
				// This is expected in Kubernetes when multiple controllers reconcile the same object.
				return ctrl.Result{Requeue: true}, nil
			}
			monitoring.RecordSpanError(span, err)
			r.Recorder.Eventf(
				shard,
				"Warning",
				"FinalizerFailed",
				"Failed to add finalizer: %v",
				err,
			)
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Register database in topology
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "ShardData.RegisterDatabaseInTopology")
		if err := r.registerDatabaseInTopology(ctx, shard); err != nil {
			// During cluster startup the toposerver may not be ready yet.
			// Silently requeue during the grace period to avoid noisy error metrics.
			resourceAge := time.Since(shard.CreationTimestamp.Time)
			if topo.IsTopoUnavailable(err) && resourceAge < topoUnavailableGracePeriod {
				childSpan.SetAttributes(attribute.Bool("topo.unavailable_grace", true))
				childSpan.End()
				logger.V(1).Info("Topology server not available yet, requeueing",
					"resourceAge", resourceAge.Round(time.Second).String(),
					"gracePeriod", topoUnavailableGracePeriod.String(),
				)
				r.Recorder.Eventf(
					shard,
					"Normal",
					"TopologyWaiting",
					"Topology server not available yet (age %s), will retry",
					resourceAge.Round(time.Second),
				)
				return ctrl.Result{RequeueAfter: topoUnavailableRequeueDelay}, nil
			}

			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			r.Recorder.Eventf(
				shard,
				"Warning",
				"TopologyError",
				"Failed to register database in topology: %v",
				err,
			)
			logger.Error(err, "Failed to register database in topology")
			return ctrl.Result{}, err
		}
		childSpan.End()
	}

	// Mark the database as registered in topology so deletion knows cleanup is needed.
	if !status.IsConditionTrue(shard.Status.Conditions, conditionDatabaseRegistered) {
		statusBase := shard.DeepCopy()
		status.SetCondition(&shard.Status.Conditions, metav1.Condition{
			Type:               conditionDatabaseRegistered,
			Status:             metav1.ConditionTrue,
			Reason:             "Registered",
			Message:            "Database registered in topology",
			ObservedGeneration: shard.Generation,
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, shard, client.MergeFrom(statusBase)); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting DatabaseRegistered condition: %w", err)
		}
	}

	// Update PodRoles and execute Drain State Machine
	{
		_, childSpan := monitoring.StartChildSpan(ctx, "ShardData.DrainStateMachine")
		podList := &corev1.PodList{}
		// Match pods belonging to this Shard CR specifically
		lbls := map[string]string{
			metadata.LabelMultigresCluster: shard.Labels[metadata.LabelMultigresCluster],
			metadata.LabelMultigresShard:   string(shard.Spec.ShardName),
		}
		if err := r.List(
			ctx,
			podList,
			client.InNamespace(shard.Namespace),
			client.MatchingLabels(lbls),
		); err != nil {
			logger.Error(err, "Failed to list pods for shard")
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			return ctrl.Result{}, err
		}

		store, err := r.getTopoStore(shard)
		if err == nil {
			// Update PodRoles from topology
			statusBase := shard.DeepCopy()
			poolerStatus := topo.GetPoolerStatus(ctx, store, shard)

			if shard.Status.PodRoles == nil {
				shard.Status.PodRoles = make(map[string]string)
			}
			rolesChanged := false

			for hostname, role := range poolerStatus.Roles {
				if shard.Status.PodRoles[hostname] != role {
					shard.Status.PodRoles[hostname] = role
					rolesChanged = true
				}
			}

			// Prune entries for poolers that no longer exist in the topology.
			if poolerStatus.QuerySuccess {
				for hostname := range shard.Status.PodRoles {
					if _, exists := poolerStatus.Roles[hostname]; !exists {
						delete(shard.Status.PodRoles, hostname)
						rolesChanged = true
					}
				}
			}

			if rolesChanged {
				if err := r.Status().Patch(ctx, shard, client.MergeFrom(statusBase)); err != nil {
					logger.Error(err, "Failed to update shard pod roles")
				}
			}

			requeue := false
			for i := range podList.Items {
				pod := &podList.Items[i]
				if pod.Annotations[metadata.AnnotationDrainState] != "" {
					shouldRequeue, derr := drain.ExecuteDrainStateMachine(
						ctx, r.Client, r.rpcClient, r.Recorder, store, shard, pod,
					)
					if derr != nil {
						logger.Error(derr, "Failed to execute drain state machine", "pod", pod.Name)
					}
					if shouldRequeue {
						requeue = true
					}
				}
			}
			_ = store.Close()
			if requeue {
				childSpan.End()
				return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
			}
		} else {
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to get topo store, cannot update roles or execute drain")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		childSpan.End()
	}

	// Evaluate backup health via gRPC to the primary pooler.
	if r.rpcClient != nil {
		_, childSpan := monitoring.StartChildSpan(ctx, "ShardData.EvaluateBackupHealth")
		result, err := r.evaluateBackupHealth(ctx, shard)
		if err != nil {
			// Backup health is observational — log and emit a warning event,
			// but do not fail the reconcile.
			monitoring.RecordSpanError(childSpan, err)
			childSpan.End()
			logger.Error(err, "Failed to evaluate backup health")
			r.Recorder.Eventf(
				shard,
				"Warning",
				"BackupCheckFailed",
				"Failed to check backup health: %v",
				err,
			)
		} else if result != nil {
			backupBase := shard.DeepCopy()
			prevHealthy := status.IsConditionTrue(
				shard.Status.Conditions,
				backuphealth.ConditionBackupHealthy,
			)
			backuphealth.ApplyBackupHealth(shard, result)

			// Emit transition events.
			if result.Healthy && !prevHealthy {
				r.Recorder.Event(shard, "Normal", "BackupHealthy", result.Message)
			} else if !result.Healthy {
				r.Recorder.Eventf(shard, "Warning", "BackupStale", result.Message)
			}

			if err := r.Status().Patch(ctx, shard, client.MergeFrom(backupBase)); err != nil {
				monitoring.RecordSpanError(childSpan, err)
				childSpan.End()
				logger.Error(err, "Failed to update shard backup status")
				return ctrl.Result{}, err
			}
			childSpan.End()
		} else {
			childSpan.End()
		}
	}

	logger.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(shard, "Normal", "Synced", "Successfully reconciled database topology")
	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when Shard is being deleted.
func (r *ShardReconciler) handleDeletion(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if slices.Contains(shard.Finalizers, finalizerName) {
		// Clean up database data from topology
		if err := r.unregisterDatabaseFromTopology(ctx, shard); err != nil {
			if topo.IsTopoUnavailable(err) {
				if !status.IsConditionTrue(shard.Status.Conditions, conditionDatabaseRegistered) {
					logger.Info("Topology never initialized, skipping cleanup")
				} else {
					deletionAge := time.Since(shard.DeletionTimestamp.Time)
					if deletionAge > topoCleanupTimeout {
						logger.Info(
							"Topology unreachable beyond cleanup timeout, forcing finalizer removal",
							"deletionAge",
							deletionAge.Round(time.Second).String(),
						)
						r.Recorder.Eventf(shard, "Warning", "CleanupSkipped",
							"Topology unreachable for %s during deletion, skipping cleanup",
							deletionAge.Round(time.Second))
					} else {
						logger.Info("Topology temporarily unreachable, will retry cleanup",
							"deletionAge", deletionAge.Round(time.Second).String())
						return ctrl.Result{RequeueAfter: topoUnavailableRequeueDelay}, nil
					}
				}
			} else {
				r.Recorder.Eventf(
					shard,
					"Warning",
					"CleanupFailed",
					"Failed to clean up database from topology: %v",
					err,
				)
				logger.Error(err, "Failed to unregister database from topology")
				return ctrl.Result{}, err
			}
		}

		// Remove finalizer
		shard.Finalizers = slices.DeleteFunc(shard.Finalizers, func(s string) bool {
			return s == finalizerName
		})
		if err := r.Update(ctx, shard); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "Failed to remove finalizer")
			r.Recorder.Eventf(
				shard,
				"Warning",
				"FinalizerFailed",
				"Failed to remove finalizer: %v",
				err,
			)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// registerDatabaseInTopology delegates database registration to the topo package.
func (r *ShardReconciler) registerDatabaseInTopology(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	store, err := r.getTopoStore(shard)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	defer func() { _ = store.Close() }()

	return topo.RegisterDatabase(ctx, store, r.Recorder, shard)
}

// unregisterDatabaseFromTopology delegates database unregistration to the topo package.
func (r *ShardReconciler) unregisterDatabaseFromTopology(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	store, err := r.getTopoStore(shard)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	defer func() { _ = store.Close() }()

	return topo.UnregisterDatabase(ctx, store, r.Recorder, shard)
}

// evaluateBackupHealth delegates backup health evaluation to the backuphealth package.
func (r *ShardReconciler) evaluateBackupHealth(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) (*backuphealth.Result, error) {
	store, err := r.getTopoStore(shard)
	if err != nil {
		return nil, fmt.Errorf("creating topology store: %w", err)
	}
	defer func() { _ = store.Close() }()

	return backuphealth.EvaluateBackupHealth(ctx, store, r.rpcClient, shard)
}

// getTopoStore returns a topology store, using the custom factory if set, otherwise the default.
func (r *ShardReconciler) getTopoStore(shard *multigresv1alpha1.Shard) (topoclient.Store, error) {
	if r.createTopoStore != nil {
		return r.createTopoStore(shard)
	}
	return topo.NewStoreFromShard(shard)
}

// SetRPCClient sets the gRPC client used for pooler management operations
// such as failover promotion and standby list updates during drain.
func (r *ShardReconciler) SetRPCClient(c rpcclient.MultiPoolerClient) {
	r.rpcClient = c
}

// SetCreateTopoStore sets a custom topology store creation function for testing.
func (r *ShardReconciler) SetCreateTopoStore(
	f func(*multigresv1alpha1.Shard) (topoclient.Store, error),
) {
	r.createTopoStore = f
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShardReconciler) SetupWithManager(mgr ctrl.Manager, opts ...controller.Options) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("shard-datahandler").
		For(&multigresv1alpha1.Shard{}).
		WithOptions(controllerOpts).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.podToShard),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return e.Object.GetAnnotations()[metadata.AnnotationDrainState] != ""
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldState := e.ObjectOld.GetAnnotations()[metadata.AnnotationDrainState]
					newState := e.ObjectNew.GetAnnotations()[metadata.AnnotationDrainState]
					return newState != "" && oldState != newState
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return false
				},
			}),
		).
		Complete(r)
}

func (r *ShardReconciler) podToShard(ctx context.Context, o client.Object) []ctrl.Request {
	for _, owner := range o.GetOwnerReferences() {
		if owner.Kind == "Shard" && strings.HasPrefix(owner.APIVersion, "multigres.com/") {
			return []ctrl.Request{
				{NamespacedName: types.NamespacedName{
					Name:      owner.Name,
					Namespace: o.GetNamespace(),
				}},
			}
		}
	}
	return nil
}
