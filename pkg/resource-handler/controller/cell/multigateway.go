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
	"github.com/numtide/multigres-operator/pkg/util/metadata"
	"github.com/numtide/multigres-operator/pkg/util/name"
)

const (
	// MultiGatewayComponentName is the component label value for MultiGateway resources
	MultiGatewayComponentName = metadata.ComponentMultiGateway

	// DefaultMultiGatewayReplicas is the default number of MultiGateway replicas
	DefaultMultiGatewayReplicas int32 = 2

	// MultiGatewayHTTPPort is the default port for HTTP connections
	MultiGatewayHTTPPort int32 = 15100

	// MultiGatewayGRPCPort is the default port for GRPC connections
	MultiGatewayGRPCPort int32 = 15170

	// MultiGatewayPostgresPort is the port for database connections,
	// used by both the container and the Kubernetes Service.
	MultiGatewayPostgresPort int32 = 5432
)

// BuildMultiGatewayDeploymentName generates the Deployment name.
// It uses DefaultConstraints (253 chars) to use readable long names.
func BuildMultiGatewayDeploymentName(cell *multigresv1alpha1.Cell) string {
	clusterName := cell.Labels["multigres.com/cluster"]
	return name.JoinWithConstraints(
		name.DefaultConstraints,
		clusterName,
		string(cell.Spec.Name),
		"multigateway",
	)
}

// BuildMultiGatewayServiceName generates the Service name.
// It uses ServiceConstraints (63 chars) for DNS safety.
func BuildMultiGatewayServiceName(cell *multigresv1alpha1.Cell) string {
	clusterName := cell.Labels["multigres.com/cluster"]
	return name.JoinWithConstraints(
		name.ServiceConstraints,
		clusterName,
		string(cell.Spec.Name),
		"multigateway",
	)
}

// BuildMultiGatewayDeployment creates a Deployment for the MultiGateway component.
func BuildMultiGatewayDeployment(
	cell *multigresv1alpha1.Cell,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	replicas := DefaultMultiGatewayReplicas
	if cell.Spec.MultiGateway.Replicas != nil {
		replicas = *cell.Spec.MultiGateway.Replicas
	}

	image := multigresv1alpha1.DefaultMultiGatewayImage
	if cell.Spec.Images.MultiGateway != "" {
		image = string(cell.Spec.Images.MultiGateway)
	}

	name := BuildMultiGatewayDeploymentName(cell)
	clusterName := cell.Labels["multigres.com/cluster"]
	labels := metadata.BuildStandardLabels(clusterName, MultiGatewayComponentName)
	metadata.AddCellLabel(labels, cell.Spec.Name)
	if cell.Spec.Zone != "" {
		metadata.AddZoneLabel(labels, cell.Spec.Zone)
	}
	if cell.Spec.Region != "" {
		metadata.AddRegionLabel(labels, cell.Spec.Region)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cell.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: metadata.GetSelectorLabels(labels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      metadata.MergeLabels(labels, cell.Spec.MultiGateway.PodLabels),
					Annotations: cell.Spec.MultiGateway.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "multigateway",
							Image: image,
							Args: []string{
								"multigateway",
								"--http-port",
								fmt.Sprintf("%d", MultiGatewayHTTPPort),
								"--grpc-port",
								fmt.Sprintf("%d", MultiGatewayGRPCPort),
								"--pg-port",
								fmt.Sprintf("%d", MultiGatewayPostgresPort),
								"--topo-global-server-addresses",
								cell.Spec.GlobalTopoServer.Address,
								"--topo-global-root",
								cell.Spec.GlobalTopoServer.RootPath,
								"--cell",
								string(cell.Spec.Name),
								"--log-level",
								string(cell.Spec.LogLevels.Multigateway),
							},
							Resources: cell.Spec.MultiGateway.Resources,
							Env:       multigresv1alpha1.BuildOTELEnvVars(cell.Spec.Observability),
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
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt32(MultiGatewayHTTPPort),
									},
								},
								PeriodSeconds:    5,
								FailureThreshold: 30,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/live",
										Port: intstr.FromInt32(MultiGatewayHTTPPort),
									},
								},
								PeriodSeconds: 10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt32(MultiGatewayHTTPPort),
									},
								},
								PeriodSeconds: 5,
							},
						},
					},
					Affinity:     cell.Spec.MultiGateway.Affinity,
					NodeSelector: buildCellNodeSelector(cell),
				},
			},
		},
	}

	if otelVol, otelMount := multigresv1alpha1.BuildOTELSamplingVolume(
		cell.Spec.Observability,
	); otelVol != nil {
		podSpec := &deployment.Spec.Template.Spec
		podSpec.Volumes = append(podSpec.Volumes, *otelVol)
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, *otelMount)
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
	name := BuildMultiGatewayServiceName(cell)
	clusterName := cell.Labels["multigres.com/cluster"]
	labels := metadata.BuildStandardLabels(clusterName, MultiGatewayComponentName)
	metadata.AddCellLabel(labels, cell.Spec.Name)
	if cell.Spec.Zone != "" {
		metadata.AddZoneLabel(labels, cell.Spec.Zone)
	}
	if cell.Spec.Region != "" {
		metadata.AddRegionLabel(labels, cell.Spec.Region)
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cell.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: metadata.GetSelectorLabels(labels),
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

// buildCellNodeSelector returns a nodeSelector map for the cell's topology.
// Returns nil if the cell has no zone or region, which means no scheduling constraint.
// Zone and region are mutually exclusive (enforced by CEL validation on CellSpec).
func buildCellNodeSelector(cell *multigresv1alpha1.Cell) map[string]string {
	switch {
	case cell.Spec.Zone != "":
		return map[string]string{
			"topology.kubernetes.io/zone": string(cell.Spec.Zone),
		}
	case cell.Spec.Region != "":
		return map[string]string{
			"topology.kubernetes.io/region": string(cell.Spec.Region),
		}
	default:
		return nil
	}
}
