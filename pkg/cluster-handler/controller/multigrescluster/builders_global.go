package multigrescluster

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// BuildGlobalTopoServer constructs the desired TopoServer for the global topology.
// It returns nil, nil if the spec does not require an Etcd server (e.g. external).
func BuildGlobalTopoServer(
	cluster *multigresv1alpha1.MultigresCluster,
	spec *multigresv1alpha1.GlobalTopoServerSpec,
	scheme *runtime.Scheme,
) (*multigresv1alpha1.TopoServer, error) {
	if spec.Etcd == nil {
		return nil, nil
	}

	if len(cluster.Name) > 63 {
		return nil, fmt.Errorf(
			"cluster name '%s' exceeds 63 characters; limit required for label validation",
			cluster.Name,
		)
	}

	ts := &multigresv1alpha1.TopoServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-global-topo",
			Namespace: cluster.Namespace,
			Labels:    map[string]string{"multigres.com/cluster": cluster.Name},
		},
		Spec: multigresv1alpha1.TopoServerSpec{
			Etcd: &multigresv1alpha1.EtcdSpec{
				Image:     spec.Etcd.Image,
				Replicas:  spec.Etcd.Replicas,
				Storage:   spec.Etcd.Storage,
				Resources: spec.Etcd.Resources,
				RootPath:  spec.Etcd.RootPath,
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, ts, scheme); err != nil {
		return nil, err
	}

	return ts, nil
}

// BuildMultiAdminDeployment constructs the desired MultiAdmin Deployment.
func BuildMultiAdminDeployment(
	cluster *multigresv1alpha1.MultigresCluster,
	spec *multigresv1alpha1.StatelessSpec,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	if len(cluster.Name) > 63 {
		return nil, fmt.Errorf(
			"cluster name '%s' exceeds 63 characters; limit required for label validation",
			cluster.Name,
		)
	}

	podLabels := map[string]string{
		"app":                   "multiadmin",
		"multigres.com/cluster": cluster.Name,
	}
	for k, v := range spec.PodLabels {
		podLabels[k] = v
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-multiadmin",
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"multigres.com/cluster": cluster.Name,
				"app":                   "multiadmin",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                   "multiadmin",
					"multigres.com/cluster": cluster.Name,
				},
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
							Image: cluster.Spec.Images.MultiAdmin,
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
