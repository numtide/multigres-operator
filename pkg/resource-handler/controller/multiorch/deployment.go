package multiorch

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
	// ComponentName is the component label value for multiorch resources
	ComponentName = "multiorch"

	// DefaultReplicas is the default number of multiorch replicas
	DefaultReplicas int32 = 1

	// DefaultImage is the default multiorch container image
	DefaultImage = "ghcr.io/multigres/multiorch:latest"
)

// BuildDeployment creates a Deployment for the MultiOrch cluster.
// Returns a deterministic Deployment based on the MultiOrch spec.
func BuildDeployment(
	multiorch *multigresv1alpha1.MultiOrch,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	replicas := DefaultReplicas
	if multiorch.Spec.Replicas != nil {
		replicas = *multiorch.Spec.Replicas
	}

	image := DefaultImage
	if multiorch.Spec.Image != "" {
		image = multiorch.Spec.Image
	}

	labels := metadata.BuildStandardLabels(multiorch.Name, ComponentName, multiorch.Spec.CellName)
	podLabels := metadata.MergeLabels(labels, multiorch.Spec.PodLabels)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      multiorch.Name,
			Namespace: multiorch.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: multiorch.Spec.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: multiorch.Spec.ServiceAccountName,
					ImagePullSecrets:   multiorch.Spec.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:      "multiorch",
							Image:     image,
							Resources: multiorch.Spec.Resources,
							Ports:     buildContainerPorts(multiorch),
						},
					},
					Affinity:                  multiorch.Spec.Affinity,
					Tolerations:               multiorch.Spec.Tolerations,
					NodeSelector:              multiorch.Spec.NodeSelector,
					TopologySpreadConstraints: multiorch.Spec.TopologySpreadConstraints,
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(multiorch, deployment, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return deployment, nil
}
