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
	// MultiGatewayComponentName is the component label value for MultiGateway resources
	MultiGatewayComponentName = "multigateway"

	// DefaultMultiGatewayReplicas is the default number of MultiGateway replicas
	DefaultMultiGatewayReplicas int32 = 2

	// DefaultMultiGatewayImage is the default MultiGateway container image
	DefaultMultiGatewayImage = "numtide/multigres-operator:latest"

	// MultiGatewayHTTPPort is the default port for HTTP connections
	MultiGatewayHTTPPort int32 = 15100

	// MultiGatewayGRPCPort is the default port for GRPC connections
	MultiGatewayGRPCPort int32 = 15170

	// MultiGatewayPostgresPort is the default port for database connections
	MultiGatewayPostgresPort int32 = 15432
)

// BuildMultiGatewayDeployment creates a Deployment for the MultiGateway component.
func BuildMultiGatewayDeployment(
	cell *multigresv1alpha1.Cell,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	replicas := DefaultMultiGatewayReplicas
	if cell.Spec.MultiGateway.Replicas != nil {
		replicas = *cell.Spec.MultiGateway.Replicas
	}

	image := DefaultMultiGatewayImage
	if cell.Spec.Images.MultiGateway != "" {
		image = cell.Spec.Images.MultiGateway
	}

	name := cell.Name + "-multigateway"
	labels := metadata.BuildStandardLabels(name, MultiGatewayComponentName)

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
							Name:      "multigateway",
							Image:     image,
							Resources: cell.Spec.MultiGateway.ResourceRequirements,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: MultiGatewayHTTPPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "grpc",
									ContainerPort: MultiGatewayGRPCPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "postgres",
									ContainerPort: MultiGatewayPostgresPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
					Affinity: cell.Spec.MultiGateway.Affinity,
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(cell, deployment, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return deployment, nil
}

// BuildMultiGatewayService creates a Service for the MultiGateway component.
func BuildMultiGatewayService(
	cell *multigresv1alpha1.Cell,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	name := cell.Name + "-multigateway"
	labels := metadata.BuildStandardLabels(name, MultiGatewayComponentName)

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
					Port:       MultiGatewayHTTPPort,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "grpc",
					Port:       MultiGatewayGRPCPort,
					TargetPort: intstr.FromString("grpc"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "postgres",
					Port:       MultiGatewayPostgresPort,
					TargetPort: intstr.FromString("postgres"),
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
