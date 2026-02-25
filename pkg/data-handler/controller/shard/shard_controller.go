package shard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/multigres/multigres/go/common/rpcclient"
	"github.com/multigres/multigres/go/common/topoclient"
	clustermetadatapb "github.com/multigres/multigres/go/pb/clustermetadata"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/numtide/multigres-operator/pkg/monitoring"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

const (
	finalizerName = "multigres.com/shard-data-protection"

	// topoUnavailableGracePeriod is the duration after resource creation during
	// which topology UNAVAILABLE errors are silently requeued instead of being
	// reported as reconcile errors. This prevents noisy error metrics during
	// normal cluster startup while the toposerver is still initializing.
	topoUnavailableGracePeriod = 2 * time.Minute

	// topoUnavailableRequeueDelay is the delay before retrying when the topology
	// server is unavailable during the grace period.
	topoUnavailableRequeueDelay = 5 * time.Second
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
			if isTopoUnavailable(err) && resourceAge < topoUnavailableGracePeriod {
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
			if shard.Status.PodRoles == nil {
				shard.Status.PodRoles = make(map[string]string)
			}
			rolesChanged := false

			cells := collectCells(shard)
			for _, cell := range cells {
				poolers, cerr := store.GetMultiPoolersByCell(ctx, cell, nil)
				if cerr == nil {
					for _, p := range poolers {
						roleName := "REPLICA"
						switch p.Type {
						case clustermetadatapb.PoolerType_PRIMARY:
							roleName = "PRIMARY"
						case clustermetadatapb.PoolerType_DRAINED:
							roleName = "DRAINED"
						}
						hostname := p.GetHostname()
						if hostname == "" {
							hostname = fmt.Sprintf("%v", p.Id)
						}

						if shard.Status.PodRoles[hostname] != roleName {
							shard.Status.PodRoles[hostname] = roleName
							rolesChanged = true
						}
					}
				}
			}

			if rolesChanged {
				if err := r.Status().Update(ctx, shard); err != nil {
					if apierrors.IsConflict(err) {
						logger.V(1).Info("Conflict updating shard pod roles, retrying")
						childSpan.End()
						return ctrl.Result{Requeue: true}, nil
					}
					logger.Error(err, "Failed to update shard pod roles")
					// continue, non-fatal for drain
				}
			}

			requeue := false
			for i := range podList.Items {
				pod := &podList.Items[i]
				if pod.Annotations[metadata.AnnotationDrainState] != "" {
					shouldRequeue, derr := r.executeDrainStateMachine(ctx, shard, pod)
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
			prevHealthy := isConditionTrue(shard.Status.Conditions, conditionBackupHealthy)
			applyBackupHealth(shard, result)

			// Emit transition events.
			if result.Healthy && !prevHealthy {
				r.Recorder.Event(shard, "Normal", "BackupHealthy", result.Message)
			} else if !result.Healthy {
				r.Recorder.Eventf(shard, "Warning", "BackupStale", result.Message)
			}

			if err := r.Status().Update(ctx, shard); err != nil {
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
			if isTopoUnavailable(err) {
				logger.Info("Topology server unreachable during deletion, skipping cleanup")
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

// registerDatabaseInTopology registers the database metadata in the global topology.
func (r *ShardReconciler) registerDatabaseInTopology(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	logger := log.FromContext(ctx)

	store, err := r.getTopoStore(shard)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	// TODO: handle store.Close() error properly
	defer func() { _ = store.Close() }()

	dbName := string(shard.Spec.DatabaseName)

	cells := collectCells(shard)
	slices.Sort(cells)

	// Build database metadata
	dbMetadata := &clustermetadatapb.Database{
		Name:             dbName,
		BackupLocation:   r.getBackupLocation(shard),
		DurabilityPolicy: r.getDurabilityPolicy(shard),
		Cells:            cells,
	}

	// Register database in topology
	if err := store.CreateDatabase(ctx, dbName, dbMetadata); err != nil {
		// Check if the error is because the database already exists
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NodeExists {
			// Database already exists — update its fields to propagate any changes
			// to backup location or cells since initial creation. UpdateDatabaseFields
			// uses atomic read-modify-write with automatic retry on version conflicts.
			if err := store.UpdateDatabaseFields(
				ctx,
				dbName,
				func(existing *clustermetadatapb.Database) error {
					existing.BackupLocation = dbMetadata.BackupLocation
					existing.Cells = dbMetadata.Cells
					return nil
				},
			); err != nil {
				return fmt.Errorf("updating existing database %s in topology: %w", dbName, err)
			}
			logger.V(1).Info("Updated existing database in topology", "database", dbName)
			return nil
		}
		r.Recorder.Eventf(
			shard,
			"Warning",
			"RegistrationFailed",
			"Failed to register database in topology: %v",
			err,
		)
		return fmt.Errorf("failed to create database in topology: %w", err)
	}

	logger.Info("Database metadata stored in topology", "database", dbName)
	r.Recorder.Eventf(
		shard,
		"Normal",
		"DatabaseRegistered",
		"Registered database '%s' in topology",
		dbName,
	)
	return nil
}

// unregisterDatabaseFromTopology removes the database metadata from the global topology.
func (r *ShardReconciler) unregisterDatabaseFromTopology(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	logger := log.FromContext(ctx)

	store, err := r.getTopoStore(shard)
	if err != nil {
		return fmt.Errorf("failed to create topology store: %w", err)
	}
	// TODO: handle store.Close() error properly
	defer func() { _ = store.Close() }()

	dbName := string(shard.Spec.DatabaseName)

	// Delete database from topology (force=false means it will fail if database still has references)
	if err := store.DeleteDatabase(ctx, dbName, false); err != nil {
		// Check if the error is because the database doesn't exist
		var topoErr topoclient.TopoError
		if errors.As(err, &topoErr) && topoErr.Code == topoclient.NoNode {
			logger.V(1).
				Info("Database does not exist in topology, skipping deletion", "database", dbName)
			return nil
		}
		if !isTopoUnavailable(err) {
			r.Recorder.Eventf(
				shard,
				"Warning",
				"UnregistrationFailed",
				"Failed to remove database from topology: %v",
				err,
			)
		}
		return fmt.Errorf("failed to delete database %s from topology: %w", dbName, err)
	}

	logger.Info("Database metadata removed from topology", "database", dbName)
	r.Recorder.Eventf(
		shard,
		"Normal",
		"DatabaseUnregistered",
		"Removed database '%s' from topology",
		dbName,
	)
	return nil
}

// getBackupLocation extracts the backup location from the shard config.
func (r *ShardReconciler) getBackupLocation(
	shard *multigresv1alpha1.Shard,
) *clustermetadatapb.BackupLocation {
	if shard.Spec.Backup != nil &&
		shard.Spec.Backup.Type == multigresv1alpha1.BackupTypeS3 &&
		shard.Spec.Backup.S3 != nil {
		return &clustermetadatapb.BackupLocation{
			Location: &clustermetadatapb.BackupLocation_S3{
				S3: &clustermetadatapb.S3Backup{
					Bucket:            shard.Spec.Backup.S3.Bucket,
					Region:            shard.Spec.Backup.S3.Region,
					Endpoint:          shard.Spec.Backup.S3.Endpoint,
					KeyPrefix:         shard.Spec.Backup.S3.KeyPrefix,
					UseEnvCredentials: shard.Spec.Backup.S3.UseEnvCredentials,
				},
			},
		}
	}

	// Default to filesystem
	path := "/backups"
	if shard.Spec.Backup != nil &&
		shard.Spec.Backup.Type == multigresv1alpha1.BackupTypeFilesystem &&
		shard.Spec.Backup.Filesystem != nil &&
		shard.Spec.Backup.Filesystem.Path != "" {
		path = shard.Spec.Backup.Filesystem.Path
	}

	return &clustermetadatapb.BackupLocation{
		Location: &clustermetadatapb.BackupLocation_Filesystem{
			Filesystem: &clustermetadatapb.FilesystemBackup{
				Path: path,
			},
		},
	}
}

// getDurabilityPolicy extracts the durability policy from the shard config.
// TODO: This should come from a field in the Shard spec once available.
func (r *ShardReconciler) getDurabilityPolicy(shard *multigresv1alpha1.Shard) string {
	// NOTE: multiorch currently only supports ANY_2 or MULTI_CELL_ANY_2 durability policies.
	// Single-node policies like NONE are not yet supported by multiorch.
	// See: https://github.com/multigres/multigres/issues/XXX
	// For now, always use ANY_2 even for single-replica setups.
	return "ANY_2"
}

// defaultCreateTopoStore creates a topoclient.Store for interacting with the global topology.
func defaultCreateTopoStore(shard *multigresv1alpha1.Shard) (topoclient.Store, error) {
	// Determine the implementation type
	implementation := shard.Spec.GlobalTopoServer.Implementation
	if implementation == "" {
		implementation = "etcd" // default to etcd
	}

	rootPath := shard.Spec.GlobalTopoServer.RootPath
	serverAddrs := []string{shard.Spec.GlobalTopoServer.Address}

	// Create config
	config := topoclient.NewDefaultTopoConfig()

	// Create topology store
	store, err := topoclient.OpenServer(implementation, rootPath, serverAddrs, config)
	if err != nil {
		return nil, fmt.Errorf("failed to open topology store: %w", err)
	}

	return store, nil
}

// getTopoStore returns a topology store, using the custom factory if set, otherwise the default.
func (r *ShardReconciler) getTopoStore(shard *multigresv1alpha1.Shard) (topoclient.Store, error) {
	if r.createTopoStore != nil {
		return r.createTopoStore(shard)
	}
	return defaultCreateTopoStore(shard)
}

// SetCreateTopoStore sets a custom topology store creation function for testing.
func (r *ShardReconciler) SetCreateTopoStore(
	f func(*multigresv1alpha1.Shard) (topoclient.Store, error),
) {
	r.createTopoStore = f
}

// isTopoUnavailable returns true if the error indicates the topology server
// is not reachable (e.g., gRPC UNAVAILABLE during startup).
func isTopoUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNAVAILABLE") ||
		strings.Contains(msg, "no connection available")
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
