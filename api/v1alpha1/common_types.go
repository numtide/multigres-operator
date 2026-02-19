/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// ============================================================================
// Shared Configuration Structs
// ============================================================================
//
// These structs are used across multiple resources (Cluster, Templates, Children)
// to ensure consistency in configuration shapes.

// StatelessSpec defines the desired state for a scalable, stateless component
// like MultiAdmin, MultiOrch, or MultiGateway.
type StatelessSpec struct {
	// Replicas is the desired number of pods.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=128
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources defines the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Affinity defines the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// PodAnnotations are annotations to add to the pods.
	// +optional
	// +kubebuilder:validation:MaxProperties=20
	// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) < 64 && size(self[k]) < 256)",message="annotation keys must be <64 chars and values <256 chars"
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// PodLabels are additional labels to add to the pods.
	// +optional
	// +kubebuilder:validation:MaxProperties=20
	// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) < 64 && size(self[k]) < 64)",message="label keys and values must be <64 chars"
	PodLabels map[string]string `json:"podLabels,omitempty"`
}

// StorageSpec defines the storage configuration.
type StorageSpec struct {
	// Size of the persistent volume.
	// +kubebuilder:validation:Pattern="^([0-9]+)(.+)$"
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Size string `json:"size,omitempty"`

	// Class is the StorageClass name.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Class string `json:"class,omitempty"`

	// AccessModes contains the desired access modes the volume should have.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// PVCRetentionPolicyType defines when to delete PVCs.
// +kubebuilder:validation:Enum=Retain;Delete
type PVCRetentionPolicyType string

const (
	// RetainPVCRetentionPolicy keeps PVCs when resources are deleted or scaled down.
	RetainPVCRetentionPolicy PVCRetentionPolicyType = "Retain"
	// DeletePVCRetentionPolicy automatically deletes PVCs when resources are deleted or scaled down.
	DeletePVCRetentionPolicy PVCRetentionPolicyType = "Delete"
)

// PVCDeletionPolicy controls PVC lifecycle management for stateful components.
type PVCDeletionPolicy struct {
	// WhenDeleted controls PVC deletion when the MultigresCluster is deleted.
	// - Retain (default): PVCs are kept for manual review and recovery
	// - Delete: PVCs are automatically deleted with the cluster
	// +optional
	// +kubebuilder:default=Retain
	WhenDeleted PVCRetentionPolicyType `json:"whenDeleted,omitempty"`

	// WhenScaled controls PVC deletion when StatefulSets are scaled down.
	// - Retain (default): PVCs from scaled-down pods are kept
	// - Delete: PVCs are automatically deleted when pods are removed
	// +optional
	// +kubebuilder:default=Retain
	WhenScaled PVCRetentionPolicyType `json:"whenScaled,omitempty"`
}

// ContainerConfig defines generic container configuration.
type ContainerConfig struct {
	// Resources defines the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ============================================================================
// Backup Configuration Types
// ============================================================================

// BackupType defines the backup storage backend.
// +kubebuilder:validation:Enum=filesystem;s3
type BackupType string

const (
	BackupTypeFilesystem BackupType = "filesystem"
	BackupTypeS3         BackupType = "s3"
)

// BackupConfig defines the pgBackRest backup configuration.
// +kubebuilder:validation:XValidation:rule="self.type != 's3' || has(self.s3)",message="s3 config is required when type is 's3'"
// +kubebuilder:validation:XValidation:rule="self.type != 'filesystem' || has(self.filesystem)",message="filesystem config is required when type is 'filesystem'"
type BackupConfig struct {
	// Type is the backup storage backend (filesystem or s3).
	Type BackupType `json:"type"`

	// Filesystem defines configuration for local/PVC-based backups.
	// Required when type is "filesystem".
	// +optional
	Filesystem *FilesystemBackupConfig `json:"filesystem,omitempty"`

	// S3 defines the S3-compatible storage configuration.
	// Required when type is "s3".
	// +optional
	S3 *S3BackupConfig `json:"s3,omitempty"`
}

// FilesystemBackupConfig defines settings for filesystem-based backups.
type FilesystemBackupConfig struct {
	// Path is the filesystem directory for backups.
	// Defaults to "/backups".
	// +optional
	Path string `json:"path,omitempty"`

	// Storage defines the PVC configuration for the backup volume.
	// This volume is shared by all pools in the shard (per-cell).
	// +optional
	Storage StorageSpec `json:"storage,omitempty"`
}

// S3BackupConfig defines S3-compatible backup storage settings.
type S3BackupConfig struct {
	Bucket            string `json:"bucket"`
	Region            string `json:"region"`
	Endpoint          string `json:"endpoint,omitempty"`
	KeyPrefix         string `json:"keyPrefix,omitempty"`
	UseEnvCredentials bool   `json:"useEnvCredentials,omitempty"`

	// CredentialsSecret is the name of the Secret containing AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

// ============================================================================
// Domain Specific Types (Strong Typing)
// ============================================================================

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=30
type DatabaseName string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=25
type TableGroupName string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=25
type ShardName string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=25
type PoolName string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=30
type CellName string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
type Zone string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
type Region string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
type TemplateRef string

// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=512
type ImageRef string

// +kubebuilder:validation:Enum=readWrite;readOnly
type PoolType string

// +kubebuilder:validation:Enum=Initializing;Progressing;Healthy;Degraded;Unknown
type Phase string

const (
	PhaseInitializing Phase = "Initializing" // Resource is being created
	PhaseProgressing  Phase = "Progressing"  // Desired state not yet achieved (rolling update)
	PhaseHealthy      Phase = "Healthy"      // Desired state reached
	PhaseDegraded     Phase = "Degraded"     // Resource is failing / crashing
	PhaseUnknown      Phase = "Unknown"
)

// MergePVCDeletionPolicy merges child and parent policies with child taking precedence.
// If child is nil, returns parent. If both nil, returns nil (caller uses default Retain).
func MergePVCDeletionPolicy(child, parent *PVCDeletionPolicy) *PVCDeletionPolicy {
	if child == nil {
		return parent
	}

	// Child exists, create merged policy
	merged := &PVCDeletionPolicy{}

	// Merge WhenDeleted
	if child.WhenDeleted != "" {
		merged.WhenDeleted = child.WhenDeleted
	} else if parent != nil {
		merged.WhenDeleted = parent.WhenDeleted
	}

	// Merge WhenScaled
	if child.WhenScaled != "" {
		merged.WhenScaled = child.WhenScaled
	} else if parent != nil {
		merged.WhenScaled = parent.WhenScaled
	}

	// If merged is empty, return nil (let caller use defaults)
	if merged.WhenDeleted == "" && merged.WhenScaled == "" {
		return nil
	}

	return merged
}

// MergeBackupConfig merges child and parent backup config with child taking precedence.
// Implements deep merge logic where appropriate.
func MergeBackupConfig(child, parent *BackupConfig) *BackupConfig {
	if child == nil && parent == nil {
		return nil
	}
	if child == nil {
		return parent.DeepCopy()
	}
	if parent == nil {
		return child.DeepCopy()
	}

	// Start with parent config as base
	merged := parent.DeepCopy()

	// If child changes the type, it fully replaces the parent config
	if child.Type != "" && child.Type != parent.Type {
		return child.DeepCopy()
	}

	// If types match (or child adopts parent type), merge details
	if child.Type != "" {
		merged.Type = child.Type
	}

	switch merged.Type {
	case BackupTypeFilesystem:
		if merged.Filesystem == nil {
			merged.Filesystem = &FilesystemBackupConfig{}
		}
		if child.Filesystem != nil {
			if child.Filesystem.Path != "" {
				merged.Filesystem.Path = child.Filesystem.Path
			}
			// Storage spec replacement
			if child.Filesystem.Storage.Size != "" {
				merged.Filesystem.Storage.Size = child.Filesystem.Storage.Size
			}
			if child.Filesystem.Storage.Class != "" {
				merged.Filesystem.Storage.Class = child.Filesystem.Storage.Class
			}
			if len(child.Filesystem.Storage.AccessModes) > 0 {
				merged.Filesystem.Storage.AccessModes = child.Filesystem.Storage.AccessModes
			}
		}
	case BackupTypeS3:
		if merged.S3 == nil {
			merged.S3 = &S3BackupConfig{}
		}
		if child.S3 != nil {
			if child.S3.Bucket != "" {
				merged.S3.Bucket = child.S3.Bucket
			}
			if child.S3.Region != "" {
				merged.S3.Region = child.S3.Region
			}
			if child.S3.Endpoint != "" {
				merged.S3.Endpoint = child.S3.Endpoint
			}
			if child.S3.KeyPrefix != "" {
				merged.S3.KeyPrefix = child.S3.KeyPrefix
			}
			// Bool fields are tricky in merge (is false explicitly set or default?)
			// For simplicity in v1alpha1, we assume if struct is present, we take the value
			merged.S3.UseEnvCredentials = child.S3.UseEnvCredentials
		}
	}

	return merged
}
