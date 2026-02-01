package tablegroup

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	l := log.FromContext(ctx)

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

	activeShardNames := make(map[string]bool, len(tg.Spec.Shards))

	for _, shardSpec := range tg.Spec.Shards {
		desired, err := BuildShard(tg, &shardSpec, r.Scheme)
		if err != nil {
			l.Error(err, "Failed to build shard", "shard", shardSpec.Name)
			if r.Recorder != nil {
				r.Recorder.Eventf(tg, "Warning", "FailedApply", "Failed to build shard %s: %v", shardSpec.Name, err)
			}
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
			if r.Recorder != nil {
				r.Recorder.Eventf(tg, "Warning", "FailedApply", "Failed to apply shard %s: %v", desired.Name, err)
			}
			return ctrl.Result{}, fmt.Errorf("failed to apply shard: %w", err)
		}

		if r.Recorder != nil {
			r.Recorder.Eventf(tg, "Normal", "Applied", "Applied Shard %s", desired.Name)
		}
	}

	// Prune orphan Shards
	existingShards := &multigresv1alpha1.ShardList{}
	if err := r.List(ctx, existingShards, client.InNamespace(tg.Namespace), client.MatchingLabels{
		"multigres.com/cluster":    tg.Labels["multigres.com/cluster"],
		"multigres.com/database":   string(tg.Spec.DatabaseName),
		"multigres.com/tablegroup": string(tg.Spec.TableGroupName),
	}); err != nil {
		if r.Recorder != nil {
			r.Recorder.Eventf(tg, "Warning", "CleanUpError", "Failed to list shards for pruning: %v", err)
		}
		return ctrl.Result{}, fmt.Errorf("failed to list shards for pruning: %w", err)
	}

	for _, s := range existingShards.Items {
		if !activeShardNames[s.Name] {
			if err := r.Delete(ctx, &s); err != nil {
				if r.Recorder != nil {
					r.Recorder.Eventf(tg, "Warning", "CleanUpError", "Failed to delete orphan shard %s: %v", s.Name, err)
				}
				return ctrl.Result{}, fmt.Errorf(
					"failed to delete orphan shard '%s': %w",
					s.Name,
					err,
				)
			}
			if r.Recorder != nil {
				r.Recorder.Eventf(tg, "Normal", "Deleted", "Deleted orphaned Shard %s", s.Name)
			}
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
		if r.Recorder != nil {
			r.Recorder.Eventf(tg, "Warning", "StatusError", "Failed to list shards for status: %v", err)
		}
		return ctrl.Result{}, fmt.Errorf("failed to list shards for status: %w", err)
	}

	for _, s := range existingShards.Items {
		for _, cond := range s.Status.Conditions {
			if cond.Type == "Available" && cond.Status == "True" {
				ready++
				break
			}
		}
	}

	tg.Status.TotalShards = total
	tg.Status.ReadyShards = ready

	condStatus := metav1.ConditionFalse
	if ready == total && total > 0 {
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

	if err := r.Status().Update(ctx, tg); err != nil {
		if r.Recorder != nil {
			r.Recorder.Eventf(tg, "Warning", "StatusError", "Failed to update status: %v", err)
		}
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	if r.Recorder != nil {
		r.Recorder.Event(tg, "Normal", "Synced", "Successfully reconciled TableGroup")
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TableGroupReconciler) SetupWithManager(
	mgr ctrl.Manager,
	opts ...controller.Options,
) error {
	controllerOpts := controller.Options{}
	if len(opts) > 0 {
		controllerOpts = opts[0]
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multigresv1alpha1.TableGroup{}).
		Owns(&multigresv1alpha1.Shard{}).
		WithOptions(controllerOpts).
		Complete(r)
}
