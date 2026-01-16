package multigrescluster

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// reconcileGlobalTopoServer reconciles the global TopoServer resource.
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

	desired, err := BuildGlobalTopoServer(cluster, spec, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build global topo server: %w", err)
	}

	// If desired is nil, it means we don't need a managed TopoServer (e.g. external).
	// We should ensure any existing managed TopoServer is cleaned up?
	// For now, if nil, we just return (logic per current implementation which didn't delete either).
	// TODO: Consider if we should delete existing if switching to external.
	if desired == nil {
		return nil
	}

	existing := &multigresv1alpha1.TopoServer{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create global topo server: %w", err)
			}
			r.Recorder.Eventf(
				cluster,
				"Normal",
				"Created",
				"Created Global TopoServer %s",
				desired.Name,
			)
			return nil
		}
		return fmt.Errorf("failed to get global topo server: %w", err)
	}

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update global topo server: %w", err)
	}

	return nil
}

// reconcileMultiAdmin reconciles the MultiAdmin Deployment.
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

	desired, err := BuildMultiAdminDeployment(cluster, spec, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to build multiadmin deployment: %w", err)
	}

	existing := &appsv1.Deployment{}
	err = r.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: desired.Name},
		existing,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create multiadmin deployment: %w", err)
			}
			r.Recorder.Eventf(
				cluster,
				"Normal",
				"Created",
				"Created MultiAdmin Deployment %s",
				desired.Name,
			)
			return nil
		}
		return fmt.Errorf("failed to get multiadmin deployment: %w", err)
	}

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update multiadmin deployment: %w", err)
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
