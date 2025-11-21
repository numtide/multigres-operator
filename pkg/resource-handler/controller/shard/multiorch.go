package shard

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

const (
	// MultiOrchComponentName is the component label value for MultiOrch resources
	MultiOrchComponentName = "multiorch"

	// DefaultMultiOrchReplicas is the default number of MultiOrch replicas
	DefaultMultiOrchReplicas int32 = 2
)

// BuildMultiOrchDeployment creates a Deployment for the MultiOrch component.
// MultiOrch handles orchestration for the shard.
func BuildMultiOrchDeployment(
	shard *multigresv1alpha1.Shard,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	replicas := DefaultMultiOrchReplicas

	name := shard.Name + "-multiorch"
	// MultiOrch doesn't have a specific cell, use default
	labels := metadata.BuildStandardLabels(name, MultiOrchComponentName, metadata.DefaultCellName)

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
						buildMultiOrchContainer(shard),
					},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(shard, deployment, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return deployment, nil
}

// BuildMultiOrchService creates a Service for the MultiOrch component.
func BuildMultiOrchService(
	shard *multigresv1alpha1.Shard,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	name := shard.Name + "-multiorch"
	labels := metadata.BuildStandardLabels(name, MultiOrchComponentName, metadata.DefaultCellName)

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
