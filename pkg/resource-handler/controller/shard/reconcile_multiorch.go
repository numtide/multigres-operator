package shard

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
)

// reconcileMultiOrchDeployment creates or updates the MultiOrch Deployment for a specific cell.
func (r *ShardReconciler) reconcileMultiOrchDeployment(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellName string,
) error {
	desired, err := BuildMultiOrchDeployment(shard, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiOrch Deployment: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply MultiOrch Deployment: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}

// reconcileMultiOrchService creates or updates the MultiOrch Service for a specific cell.
func (r *ShardReconciler) reconcileMultiOrchService(
	ctx context.Context,
	shard *multigresv1alpha1.Shard,
	cellName string,
) error {
	desired, err := BuildMultiOrchService(shard, cellName, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build MultiOrch Service: %w", err)
	}

	// Server Side Apply
	desired.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply MultiOrch Service: %w", err)
	}

	r.Recorder.Eventf(
		shard,
		"Normal",
		"Applied",
		"Applied %s %s",
		desired.GroupVersionKind().Kind,
		desired.Name,
	)

	return nil
}
