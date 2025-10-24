package multigateway

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
	// ComponentName is the component label value for MultiGateway resources
	ComponentName = "multigateway"

	// DefaultReplicas is the default number of MultiGateway replicas
	DefaultReplicas int32 = 2

	// DefaultImage is the default etcd container image
	DefaultImage = "numtide/multigres-operator:latest"
)

// BuildDeployment creates a Deployment for the Etcd cluster.
// Returns a deterministic Deployment based on the Etcd spec.
func BuildDeployment(
	mg *multigresv1alpha1.MultiGateway,
	scheme *runtime.Scheme,
) (*appsv1.Deployment, error) {
	replicas := DefaultReplicas
	// TODO: Debatable whether this defaulting makes sense.
	if mg.Spec.Replicas != nil {
		replicas = *mg.Spec.Replicas
	}

	image := DefaultImage
	if mg.Spec.Image != "" {
		image = mg.Spec.Image
	}

	labels := metadata.BuildStandardLabels(mg.Name, ComponentName, mg.Spec.CellName)
	podLabels := metadata.MergeLabels(labels, mg.Spec.PodLabels)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mg.Name,
			Namespace: mg.Namespace,
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
					Annotations: mg.Spec.PodAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: mg.Spec.ServiceAccountName,
					ImagePullSecrets:   mg.Spec.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:      "multigateway",
							Image:     image,
							Resources: mg.Spec.Resources,
							Env:       buildContainerEnv(),
							Ports:     buildContainerPorts(mg),
						},
					},
					Affinity:                  mg.Spec.Affinity,
					Tolerations:               mg.Spec.Tolerations,
					NodeSelector:              mg.Spec.NodeSelector,
					TopologySpreadConstraints: mg.Spec.TopologySpreadConstraints,
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(mg, deployment, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	return deployment, nil
}
