package tablegroup

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// TableGroupReconciler reconciles a TableGroup object.
type TableGroupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile reads the state of the TableGroup and ensures its child Shards are in the desired state.
//
// +kubebuilder:rbac:groups=multigres.com,resources=tablegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multigres.com,resources=tablegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multigres.com,resources=tablegroups/finalizers,verbs=update
// +kubebuilder:rbac:groups=multigres.com,resources=shards,verbs=get;list;watch;create;update;patch;delete
func (r *TableGroupReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	start := time.Now()
	l := log.FromContext(ctx)
	l.V(1).Info("reconcile started")

	tg := &multigresv1alpha1.TableGroup{}
	err := r.Get(ctx, req.NamespacedName, tg)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get TableGroup: %w", err)
	}

	// Reconcile Loop: Create/Update/Prune Shards
	// If being deleted, let Kubernetes GC handle cleanup
	if !tg.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	oldPhase := tg.Status.Phase

	activeShardNames := make(map[string]bool, len(tg.Spec.Shards))

	for _, shardSpec := range tg.Spec.Shards {
		desired, err := BuildShard(tg, &shardSpec, r.Scheme)
		if err != nil {
			l.Error(err, "Failed to build shard", "shard", shardSpec.Name)
			r.Recorder.Eventf(
				tg,
				"Warning",
				"FailedApply",
				"Failed to build shard %s: %v",
				shardSpec.Name,
				err,
			)
			return ctrl.Result{}, fmt.Errorf("failed to build shard: %w", err)
		}

		// Use the ACTUAL name that BuildShard creates, not a manually computed one
		activeShardNames[desired.Name] = true

		// Use Server-Side Apply
		desired.SetGroupVersionKind(multigresv1alpha1.GroupVersion.WithKind("Shard"))
		if err := r.Patch(
			ctx,
			desired,
			client.Apply,
			client.ForceOwnership,
			client.FieldOwner("multigres-operator"),
		); err != nil {
			l.Error(err, "Failed to apply shard", "shard", desired.Name)
			r.Recorder.Eventf(
				tg,
				"Warning",
				"FailedApply",
				"Failed to apply shard %s: %v",
				desired.Name,
				err,
			)
			return ctrl.Result{}, fmt.Errorf("failed to apply shard: %w", err)
		}

		r.Recorder.Eventf(tg, "Normal", "Applied", "Applied Shard %s", desired.Name)
	}

	// Prune orphan Shards
	existingShards := &multigresv1alpha1.ShardList{}
	if err := r.List(ctx, existingShards, client.InNamespace(tg.Namespace), client.MatchingLabels{
		"multigres.com/cluster":    tg.Labels["multigres.com/cluster"],
		"multigres.com/database":   string(tg.Spec.DatabaseName),
		"multigres.com/tablegroup": string(tg.Spec.TableGroupName),
	}); err != nil {
		r.Recorder.Eventf(
			tg,
			"Warning",
			"CleanUpError",
			"Failed to list shards for pruning: %v",
			err,
		)
		return ctrl.Result{}, fmt.Errorf("failed to list shards for pruning: %w", err)
	}

	for _, s := range existingShards.Items {
		if !activeShardNames[s.Name] {
			if err := r.Delete(ctx, &s); err != nil {
				r.Recorder.Eventf(
					tg,
					"Warning",
					"CleanUpError",
					"Failed to delete orphan shard %s: %v",
					s.Name,
					err,
				)
				return ctrl.Result{}, fmt.Errorf(
					"failed to delete orphan shard '%s': %w",
					s.Name,
					err,
				)
			}
			r.Recorder.Eventf(tg, "Normal", "Deleted", "Deleted orphaned Shard %s", s.Name)
		}
	}

	// Update Status
	total := int32(len(tg.Spec.Shards))
	ready := int32(0)

	// Re-list to check status
	if err := r.List(ctx, existingShards, client.InNamespace(tg.Namespace), client.MatchingLabels{
		"multigres.com/cluster":    tg.Labels["multigres.com/cluster"],
		"multigres.com/database":   string(tg.Spec.DatabaseName),
		"multigres.com/tablegroup": string(tg.Spec.TableGroupName),
	}); err != nil {
		r.Recorder.Eventf(
			tg,
			"Warning",
			"StatusError",
			"Failed to list shards for status: %v",
			err,
		)
		return ctrl.Result{}, fmt.Errorf("failed to list shards for status: %w", err)
	}

	var anyDegraded bool

	for _, s := range existingShards.Items {
		switch {
		case s.Status.ObservedGeneration != s.Generation:
			// Shard is stale/progressing, not ready
		case s.Status.Phase == multigresv1alpha1.PhaseHealthy:
			ready++
		case s.Status.Phase == multigresv1alpha1.PhaseDegraded:
			anyDegraded = true
		default: // Initializing, Progressing, Unknown
			// consider progressing
		}
	}

	tg.Status.TotalShards = total
	tg.Status.ReadyShards = ready

	switch {
	case total == 0:
		tg.Status.Phase = multigresv1alpha1.PhaseInitializing
		tg.Status.Message = "No Shards"
	case anyDegraded:
		tg.Status.Phase = multigresv1alpha1.PhaseDegraded
		tg.Status.Message = "At least one shard is degraded"
	case ready == total:
		tg.Status.Phase = multigresv1alpha1.PhaseHealthy
		tg.Status.Message = "Ready"
	default:
		tg.Status.Phase = multigresv1alpha1.PhaseProgressing
		tg.Status.Message = fmt.Sprintf("%d/%d shards ready", ready, total)
	}

	condStatus := metav1.ConditionFalse
	if tg.Status.Phase == multigresv1alpha1.PhaseHealthy {
		condStatus = metav1.ConditionTrue
	} else if total == 0 {
		condStatus = metav1.ConditionTrue
	}

	meta.SetStatusCondition(&tg.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             condStatus,
		Reason:             "ShardsReady",
		Message:            fmt.Sprintf("%d/%d shards ready", ready, total),
		LastTransitionTime: metav1.Now(),
	})

	tg.Status.ObservedGeneration = tg.Generation

	// 1. Construct the Patch Object
	patchObj := &multigresv1alpha1.TableGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: multigresv1alpha1.GroupVersion.String(),
			Kind:       "TableGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      tg.Name,
			Namespace: tg.Namespace,
		},
		Status: tg.Status,
	}

	// 2. Apply the Patch
	if oldPhase != tg.Status.Phase {
		r.Recorder.Eventf(
			tg,
			"Normal",
			"PhaseChange",
			"Transitioned from '%s' to '%s'",
			oldPhase,
			tg.Status.Phase,
		)
	}

	// Note: We rely on Server-Side Apply (SSA) to handle idempotency.
	// If the status hasn't changed, the API server will treat this Patch as a no-op,
	// so we don't need a manual DeepEqual check here.
	if err := r.Status().Patch(
		ctx,
		patchObj,
		client.Apply,
		client.FieldOwner("multigres-operator"),
		client.ForceOwnership,
	); err != nil {
		r.Recorder.Eventf(tg, "Warning", "StatusError", "Failed to patch status: %v", err)
		return ctrl.Result{}, fmt.Errorf("failed to patch status: %w", err)
	}

	l.V(1).Info("reconcile complete", "duration", time.Since(start).String())
	r.Recorder.Event(tg, "Normal", "Synced", "Successfully reconciled TableGroup")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TableGroupReconciler) SetupWithManager(
	mgr ctrl.Manager,
	opts ...controller.Options,
) error {
	controllerOpts := controller.Options{
		MaxConcurrentReconciles: 20,
	}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.TableGroup{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&multigresv1alpha1.Shard{}).
		WithOptions(controllerOpts).
		Complete(r)
}
