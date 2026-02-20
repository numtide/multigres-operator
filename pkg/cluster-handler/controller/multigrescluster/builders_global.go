package multigrescluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

// BuildGlobalTopoServer constructs the desired TopoServer for the global topology.
// Note: We do NOT use safe hashing here because GlobalTopo is a singleton resource
// per cluster with a predictable name pattern that is unlikely to exceed length limits.
// It returns nil, nil if the spec does not require an Etcd server (e.g. external).
func BuildGlobalTopoServer(
	cluster *multigresv1alpha1.MultigresCluster,
	spec *multigresv1alpha1.GlobalTopoServerSpec,
	scheme *runtime.Scheme,
) (*multigresv1alpha1.TopoServer, error) {
	if spec == nil {
		return nil, nil
	}

	// Only build TopoServer for etcd-based backends. External topo servers are managed externally.
	if spec.Etcd == nil {
		return nil, nil
	}

	labels := metadata.BuildStandardLabels(cluster.Name, metadata.ComponentGlobalTopo)
	metadata.AddClusterLabel(labels, cluster.Name)

	// Merge hierarchy: Etcd → GlobalTopoServer → MultigresCluster

	// 1. Merge Cluster and GlobalTopo
	mergedGlobal := multigresv1alpha1.MergePVCDeletionPolicy(
		spec.PVCDeletionPolicy,
		cluster.Spec.PVCDeletionPolicy,
	)

	// 2. Merge Etcd and Result 1
	var etcdPolicy *multigresv1alpha1.PVCDeletionPolicy
	if spec.Etcd != nil {
		etcdPolicy = spec.Etcd.PVCDeletionPolicy
	}

	finalPolicy := multigresv1alpha1.MergePVCDeletionPolicy(etcdPolicy, mergedGlobal)

	ts := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-global-topo",
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: multigresv1alpha1.TopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{
				Image:     spec.Etcd.Image,
				Replicas:  spec.Etcd.Replicas,
				Storage:   spec.Etcd.Storage,
				Resources: spec.Etcd.Resources,
				RootPath:  spec.Etcd.RootPath,
			},
			PVCDeletionPolicy: finalPolicy,
		},
	}

	if err := controllerutil.SetControllerReference(cluster, ts, scheme); err != nil {
		return nil, err
	}

	return ts, nil
}

// BuildMultiAdminDeployment constructs the desired MultiAdmin Deployment.
// Note: We do NOT use safe hashing here because MultiAdmin is a singleton resource
// per cluster with a predictable name pattern that is unlikely to exceed length limits.
func BuildMultiAdminDeployment(
	cluster *multigresv1alpha1.MultigresCluster,
	spec *multigresv1alpha1.StatelessSpec,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	standardLabels := metadata.BuildStandardLabels(cluster.Name, metadata.ComponentMultiAdmin)
	metadata.AddClusterLabel(standardLabels, cluster.Name)

	// Merge with user provided pod labels, but standard labels take precedence
	podLabels := metadata.MergeLabels(standardLabels, spec.PodLabels)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-multiadmin", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    standardLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: metadata.GetSelectorLabels(standardLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: spec.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:  "multiadmin",
							Image: string(cluster.Spec.Images.MultiAdmin),
							Command: []string{
								"/multigres/bin/multiadmin",
							},
							Args: []string{
								"--http-port=18000",
								"--grpc-port=18070",
								fmt.Sprintf(
									"--topo-global-server-addresses=%s-global-topo.%s.svc:2379",
									cluster.Name,
									cluster.Namespace,
								),
								"--topo-global-root=/multigres/global",
								"--service-map=grpc-multiadmin",
								"--pprof-http=true",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 18000,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "grpc",
									ContainerPort: 18070,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: spec.Resources,
							Env: multigresv1alpha1.BuildOTELEnvVars(
								cluster.Spec.Observability,
							),
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/live",
										Port: intstr.FromInt(18000),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt(18000),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
					},
					Affinity: spec.Affinity,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, deploy, scheme); err != nil {
		return nil, err
	}

	return deploy, nil
}

// BuildMultiAdminService constructs the desired Service for MultiAdmin.
func BuildMultiAdminService(
	cluster *multigresv1alpha1.MultigresCluster,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	standardLabels := metadata.BuildStandardLabels(cluster.Name, metadata.ComponentMultiAdmin)
	metadata.AddClusterLabel(standardLabels, cluster.Name)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-multiadmin", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    standardLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: metadata.GetSelectorLabels(standardLabels),
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       18000,
					TargetPort: intstr.FromInt(18000),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "grpc",
					Port:       18070,
					TargetPort: intstr.FromInt(18070),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, svc, scheme); err != nil {
		return nil, err
	}

	return svc, nil
}

// BuildMultiAdminWebDeployment constructs the desired MultiAdminWeb Deployment.
func BuildMultiAdminWebDeployment(
	cluster *multigresv1alpha1.MultigresCluster,
	spec *multigresv1alpha1.StatelessSpec,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	standardLabels := metadata.BuildStandardLabels(cluster.Name, metadata.ComponentMultiAdminWeb)
	metadata.AddClusterLabel(standardLabels, cluster.Name)

	// Merge with user provided pod labels, but standard labels take precedence
	podLabels := metadata.MergeLabels(standardLabels, spec.PodLabels)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-multiadmin-web", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    standardLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: metadata.GetSelectorLabels(standardLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: spec.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:  "multiadmin-web",
							Image: string(cluster.Spec.Images.MultiAdminWeb),
							Env: []corev1.EnvVar{
								{
									Name:  "HOSTNAME",
									Value: "::",
								},
								{
									Name:  "MULTIADMIN_API_URL",
									Value: fmt.Sprintf("http://%s-multiadmin:18000", cluster.Name),
								},
								{
									Name:  "POSTGRES_HOST",
									Value: fmt.Sprintf("%s-multigateway", cluster.Name),
								},
								{
									Name:  "POSTGRES_PORT",
									Value: "15432",
								},
								{
									Name:  "POSTGRES_DATABASE",
									Value: "postgres",
								},
								{
									Name:  "POSTGRES_USER",
									Value: "postgres",
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 18100,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: spec.Resources,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(18100),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(18100),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
					},
					Affinity: spec.Affinity,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, deploy, scheme); err != nil {
		return nil, err
	}

	return deploy, nil
}

// BuildMultiAdminWebService constructs the desired Service for MultiAdminWeb.
func BuildMultiAdminWebService(
	cluster *multigresv1alpha1.MultigresCluster,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	standardLabels := metadata.BuildStandardLabels(cluster.Name, metadata.ComponentMultiAdminWeb)
	metadata.AddClusterLabel(standardLabels, cluster.Name)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-multiadmin-web", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    standardLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: metadata.GetSelectorLabels(standardLabels),
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       18100,
					TargetPort: intstr.FromInt(18100),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, svc, scheme); err != nil {
		return nil, err
	}

	return svc, nil
}

// BuildMultiGatewayGlobalService constructs a cluster-level Service that selects
// all multigateway pods across all cells. This provides a stable DNS name for
// multiadmin-web to connect to PostgreSQL via any available multigateway,
// regardless of which cells exist.
func BuildMultiGatewayGlobalService(
	cluster *multigresv1alpha1.MultigresCluster,
	scheme *runtime.Scheme,
) (*corev1.Service, error) {
	labels := metadata.BuildStandardLabels(cluster.Name, metadata.ComponentMultiGateway)
	metadata.AddClusterLabel(labels, cluster.Name)

	// The selector intentionally omits the cell label so it matches
	// multigateway pods from all cells in this cluster.
	selector := map[string]string{
		metadata.LabelAppComponent: metadata.ComponentMultiGateway,
		metadata.LabelAppInstance:  cluster.Name,
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-multigateway", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       15432,
					TargetPort: intstr.FromString("postgres"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, svc, scheme); err != nil {
		return nil, err
	}

	return svc, nil
}
