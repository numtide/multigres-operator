package shard

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/cluster-handler/names"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

const (
	// MultiOrchComponentName is the component label value for MultiOrch resources
	MultiOrchComponentName = "multiorch"
)

// BuildMultiOrchDeployment creates a Deployment for the MultiOrch component in a specific cell.
// For shards spanning multiple cells, this function should be called once per cell.
// MultiOrch handles orchestration for the shard.
func BuildMultiOrchDeployment(
	shard *multigresv1alpha1.Shard,
	cellName string,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	// Default to 1 replica per cell if not specified
	replicas := int32(1)
	if shard.Spec.MultiOrch.Replicas != nil {
		replicas = *shard.Spec.MultiOrch.Replicas
	}

	name := buildMultiOrchNameWithCell(shard, cellName)
	labels := buildMultiOrchLabelsWithCell(shard, cellName)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						buildMultiOrchContainer(shard, cellName),
					},
					// TODO: Add Affinity support to MultiOrchSpec (like Cell's StatelessSpec)
					// This would allow pod affinity/anti-affinity rules for MultiOrch deployment
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(shard, deployment, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return deployment, nil
}

// BuildMultiOrchService creates a Service for the MultiOrch component in a specific cell.
func BuildMultiOrchService(
	shard *multigresv1alpha1.Shard,
	cellName string,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	name := buildMultiOrchNameWithCell(shard, cellName)
	labels := buildMultiOrchLabelsWithCell(shard, cellName)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: shard.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports:    buildMultiOrchServicePorts(),
		},
	}

	if err := ctrl.SetControllerReference(shard, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}

// buildMultiOrchNameWithCell generates the name for MultiOrch resources in a specific cell.
// Format: {cluster}-{db}-{tg}-{shard}-multiorch-{cellName}
func buildMultiOrchNameWithCell(shard *multigresv1alpha1.Shard, cellName string) string {
	// Logic: Use LOGICAL parts from Spec/Labels to avoid double hashing.
	// shard.Name is already hashed (cluster-db-tg-shard-HASH).
	clusterName := shard.Labels["multigres.com/cluster"]
	return names.JoinWithConstraints(
		names.ServiceConstraints,
		clusterName,
		shard.Spec.DatabaseName,
		shard.Spec.TableGroupName,
		shard.Spec.ShardName,
		"multiorch",
		cellName,
	)
}

// buildMultiOrchLabelsWithCell creates labels for MultiOrch resources in a specific cell.
func buildMultiOrchLabelsWithCell(
	shard *multigresv1alpha1.Shard,
	cellName string,
) map[string]string {
	name := buildMultiOrchNameWithCell(shard, cellName)
	labels := metadata.BuildStandardLabels(name, MultiOrchComponentName)
	metadata.AddCellLabel(labels, cellName)
	metadata.AddDatabaseLabel(labels, shard.Spec.DatabaseName)
	metadata.AddTableGroupLabel(labels, shard.Spec.TableGroupName)
	metadata.MergeLabels(labels, shard.GetObjectMeta().GetLabels())
	return labels
}
