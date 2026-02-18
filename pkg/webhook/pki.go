package webhook

import (
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// CASecretName is the name of the Secret that stores the CA certificate and key.
	CASecretName = "multigres-operator-ca-secret" //nolint:gosec // K8s resource name, not a credential

	// ServerSecretName is the name of the Secret that stores the webhook server certificate and key.
	ServerSecretName = "multigres-webhook-certs" //nolint:gosec // K8s resource name, not a credential

	// MutatingWebhookName is the name of the MutatingWebhookConfiguration resource.
	MutatingWebhookName = "multigres-operator-mutating-webhook-configuration"

	// ValidatingWebhookName is the name of the ValidatingWebhookConfiguration resource.
	ValidatingWebhookName = "multigres-operator-validating-webhook-configuration"
)

// PatchWebhookCABundle injects the CA bundle into both the Mutating and Validating
// webhook configurations. It tolerates NotFound errors (the configs may not exist
// during initial bootstrap or if webhooks are disabled).
func PatchWebhookCABundle(ctx context.Context, c client.Client, caBundle []byte) error {
	// Mutating
	mutating := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := c.Get(ctx, types.NamespacedName{Name: MutatingWebhookName}, mutating); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get mutating webhook config: %w", err)
		}
	} else {
		for i := range mutating.Webhooks {
			mutating.Webhooks[i].ClientConfig.CABundle = caBundle
		}
		if err := c.Update(ctx, mutating); err != nil {
			return fmt.Errorf("failed to update mutating webhook config: %w", err)
		}
	}

	// Validating
	validating := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := c.Get(
		ctx,
		types.NamespacedName{Name: ValidatingWebhookName},
		validating,
	); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get validating webhook config: %w", err)
		}
	} else {
		for i := range validating.Webhooks {
			validating.Webhooks[i].ClientConfig.CABundle = caBundle
		}
		if err := c.Update(ctx, validating); err != nil {
			return fmt.Errorf("failed to update validating webhook config: %w", err)
		}
	}

	return nil
}

// FindOperatorDeployment locates the operator's own Deployment for use as an owner
// reference on cert secrets. It first tries a label selector match, then falls back
// to an explicit name lookup. Returns (nil, nil) when no deployment is found,
// meaning secrets will be created without an owner reference.
func FindOperatorDeployment(
	ctx context.Context,
	c client.Client,
	namespace string,
	labels map[string]string,
	name string,
) (*appsv1.Deployment, error) {
	// 1. Try by label selector
	if len(labels) > 0 {
		list := &appsv1.DeploymentList{}
		if err := c.List(
			ctx,
			list,
			client.InNamespace(namespace),
			client.MatchingLabels(labels),
		); err != nil {
			return nil, fmt.Errorf("failed to list deployments by labels: %w", err)
		}
		if len(list.Items) > 1 {
			return nil, fmt.Errorf("found multiple deployments matching operator labels")
		}
		if len(list.Items) == 1 {
			return &list.Items[0], nil
		}
	}

	// 2. Try by explicit name
	if name != "" {
		dep := &appsv1.Deployment{}
		if err := c.Get(
			ctx,
			types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			},
			dep,
		); err != nil {
			if errors.IsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to get operator deployment by name: %w", err)
		}
		return dep, nil
	}

	return nil, nil
}
