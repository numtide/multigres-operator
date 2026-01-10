package handlers

import (
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/resolver"
)

// ============================================================================
// MultigresCluster Validator
// ============================================================================

// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-multigrescluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=multigresclusters,verbs=create;update,versions=v1alpha1,name=vmultigrescluster.kb.io,admissionReviewVersions=v1

// MultigresClusterValidator validates Create and Update events for MultigresClusters.
type MultigresClusterValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &MultigresClusterValidator{}

// NewMultigresClusterValidator creates a new validator for MultigresClusters.
func NewMultigresClusterValidator(c client.Client) *MultigresClusterValidator {
	return &MultigresClusterValidator{Client: c}
}

func (v *MultigresClusterValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

func (v *MultigresClusterValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}

func (v *MultigresClusterValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *MultigresClusterValidator) validate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cluster, ok := obj.(*multigresv1alpha1.MultigresCluster)
	if !ok {
		return nil, fmt.Errorf("expected MultigresCluster, got %T", obj)
	}

	// 1. Stateful Validation (Level 4): Referential Integrity
	if err := v.validateTemplatesExist(ctx, cluster); err != nil {
		return nil, err
	}

	return nil, nil
}

func (v *MultigresClusterValidator) validateTemplatesExist(
	ctx context.Context,
	cluster *multigresv1alpha1.MultigresCluster,
) error {
	check := func(kind, name string) error {
		if name == "" {
			return nil
		}

		// Identify if this reference is a "Fallback" (e.g., "default").
		// If it is, we allow it to be missing because the Resolver has hardcoded logic to handle that case.
		isFallback := false
		switch kind {
		case "CoreTemplate":
			if name == resolver.FallbackCoreTemplate {
				isFallback = true
			}
		case "CellTemplate":
			if name == resolver.FallbackCellTemplate {
				isFallback = true
			}
		case "ShardTemplate":
			if name == resolver.FallbackShardTemplate {
				isFallback = true
			}
		}

		key := types.NamespacedName{Name: name, Namespace: cluster.Namespace}
		var obj client.Object
		switch kind {
		case "CoreTemplate":
			obj = &multigresv1alpha1.CoreTemplate{}
		case "CellTemplate":
			obj = &multigresv1alpha1.CellTemplate{}
		case "ShardTemplate":
			obj = &multigresv1alpha1.ShardTemplate{}
		default:
			return fmt.Errorf("unknown template kind %s", kind)
		}

		if err := v.Client.Get(ctx, key, obj); err != nil {
			if errors.IsNotFound(err) {
				if isFallback {
					return nil
				}
				return fmt.Errorf(
					"referenced %s '%s' not found in namespace '%s'",
					kind,
					name,
					cluster.Namespace,
				)
			}
			return fmt.Errorf("failed to check %s '%s': %w", kind, name, err)
		}
		return nil
	}

	if err := check("CoreTemplate", cluster.Spec.TemplateDefaults.CoreTemplate); err != nil {
		return err
	}
	if err := check("CellTemplate", cluster.Spec.TemplateDefaults.CellTemplate); err != nil {
		return err
	}
	if err := check("ShardTemplate", cluster.Spec.TemplateDefaults.ShardTemplate); err != nil {
		return err
	}

	if cluster.Spec.MultiAdmin != nil && cluster.Spec.MultiAdmin.TemplateRef != "" {
		if err := check("CoreTemplate", cluster.Spec.MultiAdmin.TemplateRef); err != nil {
			return err
		}
	}

	for _, cell := range cluster.Spec.Cells {
		if err := check("CellTemplate", cell.CellTemplate); err != nil {
			return err
		}
	}

	for _, db := range cluster.Spec.Databases {
		for _, tg := range db.TableGroups {
			for _, shard := range tg.Shards {
				if err := check("ShardTemplate", shard.ShardTemplate); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// ============================================================================
// Template Validators (In-Use Protection)
// ============================================================================

// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-coretemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=coretemplates,verbs=delete,versions=v1alpha1,name=vcoretemplate.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-celltemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=celltemplates,verbs=delete,versions=v1alpha1,name=vcelltemplate.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-shardtemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=shardtemplates,verbs=delete,versions=v1alpha1,name=vshardtemplate.kb.io,admissionReviewVersions=v1

// TemplateValidator validates Delete events to ensure templates are not in use.
type TemplateValidator struct {
	Client client.Client
	Kind   string
}

var _ webhook.CustomValidator = &TemplateValidator{}

func NewTemplateValidator(c client.Client, kind string) *TemplateValidator {
	return &TemplateValidator{Client: c, Kind: kind}
}

func (v *TemplateValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *TemplateValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *TemplateValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// We need the Name and Namespace of the template being deleted
	metaObj, ok := obj.(client.Object)
	if !ok {
		return nil, fmt.Errorf("expected client.Object, got %T", obj)
	}
	templateName := metaObj.GetName()
	namespace := metaObj.GetNamespace()

	clusters := &multigresv1alpha1.MultigresClusterList{}
	if err := v.Client.List(ctx, clusters, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to list clusters for validation: %w", err)
	}

	for _, cluster := range clusters.Items {
		if v.isTemplateInUse(&cluster, templateName) {
			return nil, fmt.Errorf(
				"cannot delete %s '%s' because it is in use by MultigresCluster '%s'",
				v.Kind, templateName, cluster.Name,
			)
		}
	}

	return nil, nil
}

func (v *TemplateValidator) isTemplateInUse(
	cluster *multigresv1alpha1.MultigresCluster,
	name string,
) bool {
	switch v.Kind {
	case "CoreTemplate":
		if cluster.Spec.TemplateDefaults.CoreTemplate == name {
			return true
		}
		if cluster.Spec.MultiAdmin != nil && cluster.Spec.MultiAdmin.TemplateRef == name {
			return true
		}
	case "CellTemplate":
		if cluster.Spec.TemplateDefaults.CellTemplate == name {
			return true
		}
		for _, cell := range cluster.Spec.Cells {
			if cell.CellTemplate == name {
				return true
			}
		}
	case "ShardTemplate":
		if cluster.Spec.TemplateDefaults.ShardTemplate == name {
			return true
		}
		for _, db := range cluster.Spec.Databases {
			for _, tg := range db.TableGroups {
				for _, shard := range tg.Shards {
					if shard.ShardTemplate == name {
						return true
					}
				}
			}
		}
	}
	return false
}

// ============================================================================
// Child Resource Validator (Fallback)
// ============================================================================

// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-cell,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=cells,verbs=create;update;delete,versions=v1alpha1,name=vcell.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-shard,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=shards,verbs=create;update;delete,versions=v1alpha1,name=vshard.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-toposerver,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=toposervers,verbs=create;update;delete,versions=v1alpha1,name=vtoposerver.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-multigres-com-v1alpha1-tablegroup,mutating=false,failurePolicy=fail,sideEffects=None,groups=multigres.com,resources=tablegroups,verbs=create;update;delete,versions=v1alpha1,name=vtablegroup.kb.io,admissionReviewVersions=v1

// ChildResourceValidator prevents direct modification of managed child resources.
type ChildResourceValidator struct {
	exemptPrincipals []string
}

var _ webhook.CustomValidator = &ChildResourceValidator{}

func NewChildResourceValidator(exemptPrincipals ...string) *ChildResourceValidator {
	return &ChildResourceValidator{
		exemptPrincipals: exemptPrincipals,
	}
}

func (v *ChildResourceValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

func (v *ChildResourceValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}

func (v *ChildResourceValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

func (v *ChildResourceValidator) validate(
	ctx context.Context,
	obj runtime.Object,
) (admission.Warnings, error) {
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get admission request: %w", err)
	}

	if slices.Contains(v.exemptPrincipals, req.UserInfo.Username) {
		return nil, nil
	}

	// Determine kind for error message
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		// Fallback if GVK is not set on the object
		kind = "Resource"
	}

	return nil, fmt.Errorf(
		"Direct modification of %s is prohibited. This resource is managed by the MultigresCluster parent object.",
		kind,
	)
}
