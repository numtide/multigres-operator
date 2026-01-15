package multigrescluster

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

func (r *MultigresClusterReconciler) reconcileGlobalComponents(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	res *resolver.Resolver,
) error {
	if err := r.reconcileGlobalTopoServer(ctx, cluster, res); err != nil {
		return err
	}
	if err := r.reconcileMultiAdmin(ctx, cluster, res); err != nil {
		return err
	}
	return nil
}

func (r *MultigresClusterReconciler) reconcileGlobalTopoServer(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	res *resolver.Resolver,
) error {
	spec, err := res.ResolveGlobalTopo(ctx, cluster)
	if err != nil {
		r.Recorder.Event(cluster, "Warning", "TemplateMissing", err.Error())
		return fmt.Errorf("failed to resolve global topo: %w", err)
	}

	if spec.Etcd != nil {
		ts := &multigresv1alpha1.TopoServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name + "-global-topo",
				Namespace: cluster.Namespace,
				Labels:    map[string]string{"multigres.com/cluster": cluster.Name},
			},
		}
		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, ts, func() error {
			ts.Spec.Etcd = &multigresv1alpha1.EtcdSpec{
				Image:     spec.Etcd.Image,
				Replicas:  spec.Etcd.Replicas,
				Storage:   spec.Etcd.Storage,
				Resources: spec.Etcd.Resources,
			}
			return controllerutil.SetControllerReference(cluster, ts, r.Scheme)
		})
		if err != nil {
			return fmt.Errorf("failed to create/update global topo: %w", err)
		}
		if op == controllerutil.OperationResultCreated {
			r.Recorder.Eventf(cluster, "Normal", "Created", "Created Global TopoServer %s", ts.Name)
		}
	}
	return nil
}

func (r *MultigresClusterReconciler) reconcileMultiAdmin(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	res *resolver.Resolver,
) error {
	spec, err := res.ResolveMultiAdmin(ctx, cluster)
	if err != nil {
		r.Recorder.Event(cluster, "Warning", "TemplateMissing", err.Error())
		return fmt.Errorf("failed to resolve multiadmin: %w", err)
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
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec.Replicas = spec.Replicas
		deploy.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app":                   "multiadmin",
				"multigres.com/cluster": cluster.Name,
			},
		}

		podLabels := map[string]string{"app": "multiadmin", "multigres.com/cluster": cluster.Name}
		for k, v := range spec.PodLabels {
			podLabels[k] = v
		}

		deploy.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      podLabels,
				Annotations: spec.PodAnnotations,
			},
			Spec: corev1.PodSpec{
				ImagePullSecrets: cluster.Spec.Images.ImagePullSecrets,
				Containers: []corev1.Container{
					{
						Name:      "multiadmin",
						Image:     cluster.Spec.Images.MultiAdmin,
						Resources: spec.Resources,
					},
				},
				Affinity: spec.Affinity,
			},
		}
		return controllerutil.SetControllerReference(cluster, deploy, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("failed to create/update multiadmin: %w", err)
	}
	if op == controllerutil.OperationResultCreated {
		r.Recorder.Eventf(
			cluster,
			"Normal",
			"Created",
			"Created MultiAdmin Deployment %s",
			deploy.Name,
		)
	}

	return nil
}

// getGlobalTopoRef helper shared by Cells and Databases
func (r *MultigresClusterReconciler) getGlobalTopoRef(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
	res *resolver.Resolver,
) (multigresv1alpha1.GlobalTopoServerRef, error) {
	spec, err := res.ResolveGlobalTopo(ctx, cluster)
	if err != nil {
		r.Recorder.Event(cluster, "Warning", "TemplateMissing", err.Error())
		return multigresv1alpha1.GlobalTopoServerRef{}, err
	}

	address := ""
	if spec.Etcd != nil {
		address = fmt.Sprintf("%s-global-topo-client.%s.svc:2379", cluster.Name, cluster.Namespace)
	} else if spec.External != nil && len(spec.External.Endpoints) > 0 {
		address = string(spec.External.Endpoints[0])
	}

	return multigresv1alpha1.GlobalTopoServerRef{
		Address:        address,
		RootPath:       "/multigres/global",
		Implementation: "etcd2",
	}, nil
}
