package handlers

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	admissionv1 "k8s.io/api/admission/v1"
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
	// Ensure that if a DeploymentTemplate is referenced, it actually exists.
	if cluster.Spec.DeploymentTemplate != "" {
		// We only need to check this if the template field is set.
		// We do NOT check the inline ManagedSpec (Level 2 CEL handles mutual exclusion).
		dt := &multigresv1alpha1.DeploymentTemplate{}
		key := types.NamespacedName{
			Name:      cluster.Spec.DeploymentTemplate,
			Namespace: cluster.Namespace, // Templates are namespaced
		}
		if err := v.Client.Get(ctx, key, dt); err != nil {
			return admission.Denied(fmt.Sprintf("referenced DeploymentTemplate '%s' not found in namespace '%s'", key.Name, key.Namespace))
		}
	}

	// 2. Fallback Context-Aware Validation (Level 3): Safe Updates
	// Ideally handled by ValidatingAdmissionPolicy, but included here for K8s < 1.30 support.
	if req.Operation == admissionv1.Update {
		oldCluster := &multigresv1alpha1.MultigresCluster{}
		if err := v.decoder.DecodeRaw(req.OldObject, oldCluster); err == nil {
			// Example: Prevent reducing storage size (if that were a top-level field).
			// Currently most storage is in templates, but this is where we would check
			// oldCluster.Status vs newCluster.Spec if needed.
		}
	}

	return admission.Allowed("")
}

// ============================================================================
// DeploymentTemplate Validator
// ============================================================================

// DeploymentTemplateValidator validates Delete events to ensure templates are not in use.
type DeploymentTemplateValidator struct {
	Client  client.Client
	decoder admission.Decoder
}

// NewDeploymentTemplateValidator creates a new validator for DeploymentTemplates.
func NewDeploymentTemplateValidator(c client.Client) *DeploymentTemplateValidator {
	return &DeploymentTemplateValidator{Client: c}
}

// InjectDecoder injects the decoder.
func (v *DeploymentTemplateValidator) InjectDecoder(decoder admission.Decoder) error {
	v.decoder = decoder
	return nil
}

// Handle implements the admission.Handler interface.
func (v *DeploymentTemplateValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// We only care about DELETE operations for DeploymentTemplates
	if req.Operation != admissionv1.Delete {
		return admission.Allowed("")
	}

	// Note: For DELETE, req.Object is usually nil, we must use req.OldObject or just req.Name.
	templateName := req.Name
	namespace := req.Namespace

	// Stateful Validation (Level 4): Referential Integrity
	// Check if any MultigresCluster is currently using this template.
	clusters := &multigresv1alpha1.MultigresClusterList{}
	if err := v.Client.List(ctx, clusters, client.InNamespace(namespace)); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list clusters for validation: %w", err))
	}

	for _, cluster := range clusters.Items {
		if cluster.Spec.DeploymentTemplate == templateName {
			return admission.Denied(fmt.Sprintf(
				"cannot delete DeploymentTemplate '%s' because it is in use by MultigresCluster '%s'",
				templateName, cluster.Name,
			))
		}
	}

	return admission.Allowed("")
}

// ============================================================================
// Child Resource Validator (Fallback)
// ============================================================================

// ChildResourceValidator prevents direct modification of managed child resources.
// ideally, this is handled by ValidatingAdmissionPolicy (Level 3), but we implement
// it here as a fallback for clusters that do not support it or where the policy isn't installed.
type ChildResourceValidator struct {
	decoder          admission.Decoder
	exemptPrincipals []string
}

// NewChildResourceValidator creates a validator that blocks modification of child resources
// unless the user matches one of the exemptPrincipals (e.g., the operator's ServiceAccount).
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

	// ValidatingAdmissionPolicy Fallback:
	// Rule: "request.userInfo.username == 'system:serviceaccount:multigres-system:multigres-operator'"
	return admission.Denied(fmt.Sprintf(
		"Direct modification of %s is prohibited. This resource is managed by the MultigresCluster parent object.",
		req.Kind.Kind,
	))
}
