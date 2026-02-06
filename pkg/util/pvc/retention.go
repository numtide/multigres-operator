// Package pvc provides utilities for PVC lifecycle management.
package pvc

import (
	appsv1 "k8s.io/api/apps/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// BuildRetentionPolicy converts the operator's PVC deletion policy to Kubernetes StatefulSet retention policy.
// If policy is nil, defaults to Retain for both WhenDeleted and WhenScaled (safe default).
func BuildRetentionPolicy(
	policy *multigresv1alpha1.PVCDeletionPolicy,
) *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy {
	if policy == nil {
		return &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
		}
	}

	result := &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{}

	switch policy.WhenDeleted {
	case multigresv1alpha1.DeletePVCRetentionPolicy:
		result.WhenDeleted = appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	default:
		result.WhenDeleted = appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	}

	switch policy.WhenScaled {
	case multigresv1alpha1.DeletePVCRetentionPolicy:
		result.WhenScaled = appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	default:
		result.WhenScaled = appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	}

	return result
}
