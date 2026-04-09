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
	conditionStorageClassValid      = "StorageClassValid"
	storageClassDependencyRequeue   = 10 * time.Second
	storageClassMissingReason       = "StorageClassNotFound"
	storageClassPresentReason       = "StorageClassFound"
	storageClassMissingEventReason  = "MissingStorageClass"
	storageClassLookupFailedMessage = "failed to get StorageClass %q: %w"
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
	if shard.Spec.Backup == nil || shard.Spec.Backup.Type != multigresv1alpha1.BackupTypeFilesystem {
		return ""
	}
	if shard.Spec.Backup.Filesystem == nil {
		return ""
	}
	return shard.Spec.Backup.Filesystem.Storage.Class
}

func (r *ShardReconciler) ensureStorageClassExists(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	className string,
	useDescription string,
) error {
	if className == "" {
		return nil
	}

	sc := &storagev1.StorageClass{}
	err := r.Get(ctx, client.ObjectKey{Name: className}, sc)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf(storageClassLookupFailedMessage, className, err)
		}
		message := fmt.Sprintf("StorageClass %q for %s does not exist", className, useDescription)
		if setErr := r.setStorageClassCondition(
			ctx,
			shard,
			metav1.ConditionFalse,
			storageClassMissingReason,
			message,
		); setErr != nil {
			return setErr
		}
		r.Recorder.Eventf(shard, "Warning", storageClassMissingEventReason, "%s", message)
		return &missingStorageClassDependencyError{className: className}
	}

	message := fmt.Sprintf("StorageClass %q for %s exists", className, useDescription)
	return r.setStorageClassCondition(
		ctx,
		shard,
		metav1.ConditionTrue,
		storageClassPresentReason,
		message,
	)
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
