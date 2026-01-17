package multigrescluster

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
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
	if desired == nil {
		return nil
	}

	// Server Side Apply
	desired.SetGroupVersionKind(multigresv1alpha1.GroupVersion.WithKind("TopoServer"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply global topo server: %w", err)
	}

	r.Recorder.Eventf(
		cluster,
		"Normal",
		"Applied",
		"Applied Global TopoServer %s",
		desired.Name,
	)

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

	// Server Side Apply
	desired.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
	if err := r.Patch(
		ctx,
		desired,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("multigres-operator"),
	); err != nil {
		return fmt.Errorf("failed to apply multiadmin deployment: %w", err)
	}

	r.Recorder.Eventf(
		cluster,
		"Normal",
		"Applied",
		"Applied MultiAdmin Deployment %s",
		desired.Name,
	)

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
