package handlers

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
)

// ============================================================================
// MultigresCluster Validator
// ============================================================================

// MultigresClusterValidator validates Create and Update events for MultigresClusters.
type MultigresClusterValidator struct {
	Client  client.Client
	decoder admission.Decoder
}

// NewMultigresClusterValidator creates a new validator for MultigresClusters.
func NewMultigresClusterValidator(c client.Client) *MultigresClusterValidator {
	return &MultigresClusterValidator{Client: c}
}

// InjectDecoder injects the decoder.
func (v *MultigresClusterValidator) InjectDecoder(decoder admission.Decoder) error {
	v.decoder = decoder
	return nil
}

// Handle implements the admission.Handler interface.
func (v *MultigresClusterValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	cluster := &multigresv1alpha1.MultigresCluster{}
	if err := v.decoder.Decode(req, cluster); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// 1. Stateful Validation (Level 4): Referential Integrity
	// Ensure that all referenced templates actually exist.
	if err := v.validateTemplatesExist(ctx, cluster); err != nil {
		return admission.Denied(err.Error())
	}

	// 2. Fallback Context-Aware Validation (Level 3): Safe Updates
	// Ideally handled by ValidatingAdmissionPolicy, but included here for K8s < 1.30 support.
	if req.Operation == admissionv1.Update {
		// Example: Check for unsafe changes that CEL cannot easily catch involving oldObject state
		// For now, most logic is covered by CEL, but this block remains for future extension.
	}

	return admission.Allowed("")
}

func (v *MultigresClusterValidator) validateTemplatesExist(ctx context.Context, cluster *multigresv1alpha1.MultigresCluster) error {
	// Helper to check existence of a generic template
	check := func(kind, name string) error {
		if name == "" {
			return nil
		}
		key := types.NamespacedName{Name: name, Namespace: cluster.Namespace}
		// We use a partial object or Unstructured, or the actual type.
		// Since we have the types, we use them.
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
				return fmt.Errorf("referenced %s '%s' not found in namespace '%s'", kind, name, cluster.Namespace)
			}
			return fmt.Errorf("failed to check %s '%s': %w", kind, name, err)
		}
		return nil
	}

	// 1. Check Template Defaults
	if err := check("CoreTemplate", cluster.Spec.TemplateDefaults.CoreTemplate); err != nil {
		return err
	}
	if err := check("CellTemplate", cluster.Spec.TemplateDefaults.CellTemplate); err != nil {
		return err
	}
	if err := check("ShardTemplate", cluster.Spec.TemplateDefaults.ShardTemplate); err != nil {
		return err
	}

	// 2. Check Component Specific References
	if cluster.Spec.MultiAdmin.TemplateRef != "" {
		if err := check("CoreTemplate", cluster.Spec.MultiAdmin.TemplateRef); err != nil {
			return err
		}
	}

	// 3. Check Cell Templates
	for _, cell := range cluster.Spec.Cells {
		if err := check("CellTemplate", cell.CellTemplate); err != nil {
			return err
		}
	}

	// 4. Check Shard Templates
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

// TemplateValidator validates Delete events to ensure templates are not in use.
type TemplateValidator struct {
	Client client.Client
	Kind   string // "CoreTemplate", "CellTemplate", or "ShardTemplate"
}

func NewTemplateValidator(c client.Client, kind string) *TemplateValidator {
	return &TemplateValidator{Client: c, Kind: kind}
}

func (v *TemplateValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// We only care about DELETE operations
	if req.Operation != admissionv1.Delete {
		return admission.Allowed("")
	}

	templateName := req.Name
	namespace := req.Namespace

	// List all clusters in the namespace to check for usage.
	// optimization: In the future, we could rely on a tracking label on the Cluster CR.
	clusters := &multigresv1alpha1.MultigresClusterList{}
	if err := v.Client.List(ctx, clusters, client.InNamespace(namespace)); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list clusters for validation: %w", err))
	}

	for _, cluster := range clusters.Items {
		if v.isTemplateInUse(&cluster, templateName) {
			return admission.Denied(fmt.Sprintf(
				"cannot delete %s '%s' because it is in use by MultigresCluster '%s'",
				v.Kind, templateName, cluster.Name,
			))
		}
	}

	return admission.Allowed("")
}

func (v *TemplateValidator) isTemplateInUse(cluster *multigresv1alpha1.MultigresCluster, name string) bool {
	switch v.Kind {
	case "CoreTemplate":
		// Check defaults
		if cluster.Spec.TemplateDefaults.CoreTemplate == name {
			return true
		}
		// Check explicit MultiAdmin ref
		if cluster.Spec.MultiAdmin.TemplateRef == name {
			return true
		}
		// Check GlobalTopoServer (if it supported templateRef in the future, currently it doesn't in v1alpha1 spec)

	case "CellTemplate":
		// Check defaults
		if cluster.Spec.TemplateDefaults.CellTemplate == name {
			return true
		}
		// Check explicit Cell refs
		for _, cell := range cluster.Spec.Cells {
			if cell.CellTemplate == name {
				return true
			}
		}

	case "ShardTemplate":
		// Check defaults
		if cluster.Spec.TemplateDefaults.ShardTemplate == name {
			return true
		}
		// Check explicit Shard refs
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

// ChildResourceValidator prevents direct modification of managed child resources.
type ChildResourceValidator struct {
	decoder          admission.Decoder
	exemptPrincipals []string
}

// NewChildResourceValidator creates a validator that blocks modification of child resources
// unless the user matches one of the exemptPrincipals.
func NewChildResourceValidator(exemptPrincipals ...string) *ChildResourceValidator {
	return &ChildResourceValidator{
		exemptPrincipals: exemptPrincipals,
	}
}

func (v *ChildResourceValidator) InjectDecoder(decoder admission.Decoder) error {
	v.decoder = decoder
	return nil
}

func (v *ChildResourceValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Allow the operator (or other exempt principals) to make changes.
	if slices.Contains(v.exemptPrincipals, req.UserInfo.Username) {
		return admission.Allowed("")
	}

	return admission.Denied(fmt.Sprintf(
		"Direct modification of %s is prohibited. This resource is managed by the MultigresCluster parent object.",
		req.Kind.Kind,
	))
}
