package cell

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/metadata"
)

const (
	// MultiOrchComponentName is the component label value for MultiOrch resources
	MultiOrchComponentName = "multiorch"

	// DefaultMultiOrchReplicas is the default number of MultiOrch replicas
	DefaultMultiOrchReplicas int32 = 2

	// DefaultMultiOrchImage is the default MultiOrch container image
	DefaultMultiOrchImage = "numtide/multigres-operator:latest"

	// MultiOrchHTTPPort is the default port for HTTP connections
	MultiOrchHTTPPort int32 = 15200

	// MultiOrchGRPCPort is the default port for GRPC connections
	MultiOrchGRPCPort int32 = 15270
)

// BuildMultiOrchDeployment creates a Deployment for the MultiOrch component.
func BuildMultiOrchDeployment(
	cell *multigresv1alpha1.Cell,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	replicas := DefaultMultiOrchReplicas
	if cell.Spec.MultiOrch.Replicas != nil {
		replicas = *cell.Spec.MultiOrch.Replicas
	}

	image := DefaultMultiOrchImage
	if cell.Spec.Images.MultiOrch != "" {
		image = cell.Spec.Images.MultiOrch
	}

	name := cell.Name + "-multiorch"
	labels := metadata.BuildStandardLabels(name, MultiOrchComponentName)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cell.Namespace,
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
						{
							Name:      "multiorch",
							Image:     image,
							Resources: cell.Spec.MultiOrch.ResourceRequirements,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: MultiOrchHTTPPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "grpc",
									ContainerPort: MultiOrchGRPCPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
					Affinity: cell.Spec.MultiOrch.Affinity,
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(cell, deployment, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return deployment, nil
}

// BuildMultiOrchService creates a Service for the MultiOrch component.
func BuildMultiOrchService(
	cell *multigresv1alpha1.Cell,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	name := cell.Name + "-multiorch"
	labels := metadata.BuildStandardLabels(name, MultiOrchComponentName)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cell.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       MultiOrchHTTPPort,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "grpc",
					Port:       MultiOrchGRPCPort,
					TargetPort: intstr.FromString("grpc"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(cell, svc, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return svc, nil
}
