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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

const (
	finalizerName = "tablegroup.multigres.com/finalizer"
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

	// Handle Deletion
	if !tg.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, tg)
	}

	// Add Finalizer
	if !controllerutil.ContainsFinalizer(tg, finalizerName) {
		controllerutil.AddFinalizer(tg, finalizerName)
		if err := r.Update(ctx, tg); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	activeShardNames := make(map[string]bool, len(tg.Spec.Shards))

	for _, shardSpec := range tg.Spec.Shards {
		shardNameFull := fmt.Sprintf("%s-%s", tg.Name, shardSpec.Name)
		activeShardNames[shardNameFull] = true

		shardCR := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shardNameFull,
				Namespace: tg.Namespace,
				Labels: map[string]string{
					"multigres.com/cluster":    tg.Labels["multigres.com/cluster"],
					"multigres.com/database":   tg.Spec.DatabaseName,
					"multigres.com/tablegroup": tg.Spec.TableGroupName,
					"multigres.com/shard":      shardSpec.Name,
				},
			},
		}

		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, shardCR, func() error {
			shardCR.Spec.DatabaseName = tg.Spec.DatabaseName
			shardCR.Spec.TableGroupName = tg.Spec.TableGroupName
			shardCR.Spec.ShardName = shardSpec.Name
			shardCR.Spec.Images = tg.Spec.Images
			shardCR.Spec.GlobalTopoServer = tg.Spec.GlobalTopoServer
			shardCR.Spec.MultiOrch = shardSpec.MultiOrch
			shardCR.Spec.Pools = shardSpec.Pools

			return controllerutil.SetControllerReference(tg, shardCR, r.Scheme)
		})
		if err != nil {
			l.Error(err, "Failed to create/update shard", "shard", shardNameFull)
			return ctrl.Result{}, fmt.Errorf("failed to create/update shard: %w", err)
		}
		if op == controllerutil.OperationResultCreated {
			r.Recorder.Eventf(tg, "Normal", "Created", "Created Shard %s", shardCR.Name)
		}
	}

	// Prune orphan Shards
	existingShards := &multigresv1alpha1.ShardList{}
	if err := r.List(ctx, existingShards, client.InNamespace(tg.Namespace), client.MatchingLabels{
		"multigres.com/cluster":    tg.Labels["multigres.com/cluster"],
		"multigres.com/database":   tg.Spec.DatabaseName,
		"multigres.com/tablegroup": tg.Spec.TableGroupName,
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list shards for pruning: %w", err)
	}

	for _, s := range existingShards.Items {
		if !activeShardNames[s.Name] {
			if err := r.Delete(ctx, &s); err != nil {
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
		"multigres.com/database":   tg.Spec.DatabaseName,
		"multigres.com/tablegroup": tg.Spec.TableGroupName,
	}); err != nil {
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
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

func (r *TableGroupReconciler) handleDelete(
	ctx context.Context,
	tg *multigresv1alpha1.TableGroup,
) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(tg, finalizerName) {
		// List all Shards owned by this TableGroup
		shards := &multigresv1alpha1.ShardList{}
		if err := r.List(ctx, shards, client.InNamespace(tg.Namespace), client.MatchingLabels{
			"multigres.com/cluster":    tg.Labels["multigres.com/cluster"],
			"multigres.com/database":   tg.Spec.DatabaseName,
			"multigres.com/tablegroup": tg.Spec.TableGroupName,
		}); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list shards for deletion: %w", err)
		}

		// Delete them
		for _, s := range shards.Items {
			if s.GetDeletionTimestamp().IsZero() {
				if err := r.Delete(ctx, &s); err != nil {
					if !errors.IsNotFound(err) {
						return ctrl.Result{}, fmt.Errorf(
							"failed to delete shard %s: %w",
							s.Name,
							err,
						)
					}
				}
			}
		}

		// If any shards remain, wait
		if len(shards.Items) > 0 {
			r.Recorder.Eventf(
				tg,
				"Normal",
				"Cleanup",
				"Waiting for %d shards to be deleted",
				len(shards.Items),
			)
			// Requeue to check again
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}

		// All shards gone, remove finalizer
		controllerutil.RemoveFinalizer(tg, finalizerName)
		if err := r.Update(ctx, tg); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
		}
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
