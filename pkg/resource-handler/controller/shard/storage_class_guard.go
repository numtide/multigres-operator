package shard

import (
	"context"
	"errors"
	"fmt"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
)

const (
	conditionStorageClassValid    = "StorageClassValid"
	storageClassDependencyRequeue = 10 * time.Second

	storageClassNotFoundReason     = "StorageClassNotFound"
	storageClassFoundReason        = "StorageClassFound"
	storageClassNotSpecifiedReason = "StorageClassNotSpecified"
)

type missingStorageClassDependencyError struct {
	className string
}

func (e *missingStorageClassDependencyError) Error() string {
	return fmt.Sprintf("referenced StorageClass %q was not found", e.className)
}

func isMissingStorageClassDependency(err error) bool {
	var depErr *missingStorageClassDependencyError
	return errors.As(err, &depErr)
}

func backupFilesystemStorageClassName(shard *multigresv1alpha1.Shard) string {
	if shard.Spec.Backup == nil ||
		shard.Spec.Backup.Type != multigresv1alpha1.BackupTypeFilesystem {
		return ""
	}
	if shard.Spec.Backup.Filesystem == nil {
		return ""
	}
	return shard.Spec.Backup.Filesystem.Storage.Class
}

func (r *ShardReconciler) validateStorageClassExists(
	ctx context.Context,
	className string,
) (bool, error) {
	if className == "" {
		return true, nil
	}

	sc := &storagev1.StorageClass{}
	err := r.Get(ctx, client.ObjectKey{Name: className}, sc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get StorageClass %q: %w", className, err)
	}
	return true, nil
}

func (r *ShardReconciler) validateBackupStorageClassDependency(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	backupClass := backupFilesystemStorageClassName(shard)
	if backupClass == "" {
		return r.setStorageClassCondition(
			ctx,
			shard,
			metav1.ConditionTrue,
			storageClassNotSpecifiedReason,
			"No explicit backup filesystem StorageClass configured; using cluster default",
		)
	}

	exists, err := r.validateStorageClassExists(ctx, backupClass)
	if err != nil {
		return fmt.Errorf("failed to validate backup StorageClass %q: %w", backupClass, err)
	}
	if !exists {
		msg := fmt.Sprintf("StorageClass %q not found for shared backup PVCs", backupClass)
		if setErr := r.setStorageClassCondition(
			ctx,
			shard,
			metav1.ConditionFalse,
			storageClassNotFoundReason,
			msg,
		); setErr != nil {
			return setErr
		}
		r.Recorder.Eventf(shard, "Warning", storageClassNotFoundReason, msg)
		return &missingStorageClassDependencyError{className: backupClass}
	}

	return r.setStorageClassCondition(
		ctx,
		shard,
		metav1.ConditionTrue,
		storageClassFoundReason,
		fmt.Sprintf("StorageClass %q found for shared backup PVCs", backupClass),
	)
}

func (r *ShardReconciler) validatePoolStorageClassDependencies(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
) error {
	hasExplicitPoolStorageClass := false
	for poolName, pool := range shard.Spec.Pools {
		if pool.Storage.Class == "" {
			continue
		}
		hasExplicitPoolStorageClass = true

		exists, err := r.validateStorageClassExists(ctx, pool.Storage.Class)
		if err != nil {
			return fmt.Errorf(
				"failed to validate StorageClass %q for pool %s: %w",
				pool.Storage.Class,
				poolName,
				err,
			)
		}
		if !exists {
			msg := fmt.Sprintf(
				"StorageClass %q not found for pool %s",
				pool.Storage.Class,
				poolName,
			)
			if setErr := r.setStorageClassCondition(
				ctx,
				shard,
				metav1.ConditionFalse,
				storageClassNotFoundReason,
				msg,
			); setErr != nil {
				return setErr
			}
			r.Recorder.Eventf(shard, "Warning", storageClassNotFoundReason, msg)
			return &missingStorageClassDependencyError{className: pool.Storage.Class}
		}
	}

	reason := storageClassNotSpecifiedReason
	message := "No explicit pool StorageClass configured; using cluster default"
	if hasExplicitPoolStorageClass {
		reason = storageClassFoundReason
		message = "All explicit pool StorageClasses are present"
	}
	return r.setStorageClassCondition(ctx, shard, metav1.ConditionTrue, reason, message)
}

func (r *ShardReconciler) setStorageClassCondition(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	status metav1.ConditionStatus,
	reason string,
	message string,
) error {
	latest := &multigresv1alpha1.Shard{}
	key := client.ObjectKeyFromObject(shard)
	if err := r.Get(ctx, key, latest); err != nil {
		return fmt.Errorf("failed to get Shard for StorageClass condition patch: %w", err)
	}

	base := latest.DeepCopy()
	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               conditionStorageClassValid,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: latest.Generation,
	})

	if err := r.Status().Patch(ctx, latest, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("failed to patch Shard StorageClass condition: %w", err)
	}

	return nil
}
