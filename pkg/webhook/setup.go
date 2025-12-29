// Package webhook provides the entry point for the Multigres Operator's admission control layer.
//
// This package orchestrates the setup of the controller-runtime webhook server, including:
//
//  1. Certificate Management: It delegates to the 'cert' subpackage to ensure TLS certificates
//     are present (either self-signed or externally provisioned) before the server starts.
//
//  2. Handler Registration: It registers the specific admission handlers (from the 'handlers'
//     subpackage) to their corresponding API paths (e.g., /mutate-..., /validate-...).
//
// Usage:
//
//	if err := webhook.Setup(mgr, resolver, opts); err != nil {
//	    setupLog.Error(err, "unable to setup webhook")
//	    os.Exit(1)
//	}
package webhook

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/numtide/multigres-operator/pkg/resolver"
	"github.com/numtide/multigres-operator/pkg/webhook/cert"
	"github.com/numtide/multigres-operator/pkg/webhook/handlers"
)

// Options contains the configuration required to set up the webhook server.
type Options struct {
	// Enable indicates whether to start the webhook server.
	Enable bool
	// CertStrategy defines how certificates are managed ("external" or "self-signed").
	CertStrategy string
	// CertDir is the directory where certificates should be read/written.
	CertDir string
	// Namespace is the operator's namespace (required for self-signed strategy).
	Namespace string
	// ServiceName is the operator's service name (required for self-signed strategy).
	ServiceName string
}

// Setup configures the webhook server, handles certificate generation (if requested),
// and registers the admission handlers with the manager.
func Setup(mgr ctrl.Manager, res *resolver.Resolver, opts Options) error {
	if !opts.Enable {
		return nil
	}

	logger := mgr.GetLogger().WithName("webhook-setup")
	logger.Info("Setting up webhook server", "strategy", opts.CertStrategy)

	// 1. Certificate Management
	// If using self-signed certs, we must ensure they exist and patch the
	// WebhookConfigurations *before* the manager starts the server.
	if opts.CertStrategy == "self-signed" {
		certMgr := cert.NewManager(mgr.GetClient(), cert.Options{
			Namespace:   opts.Namespace,
			ServiceName: opts.ServiceName,
			CertDir:     opts.CertDir,
		})

		// Use a temporary context as the manager's context isn't started yet
		if err := certMgr.EnsureCerts(context.Background()); err != nil {
			return fmt.Errorf("failed to bootstrap self-signed certificates: %w", err)
		}
	}

	// 2. Register Webhooks
	server := mgr.GetWebhookServer()

	// -- Mutating Webhook (Defaulter) --
	// Path: /mutate-multigres-com-v1alpha1-multigrescluster
	// This path MUST match the +kubebuilder:webhook annotation in your types or controller
	server.Register(
		"/mutate-multigres-com-v1alpha1-multigrescluster",
		&webhook.Admission{
			Handler: handlers.NewMultigresClusterDefaulter(res),
		},
	)

	// -- Validating Webhook (MultigresCluster) --
	// Path: /validate-multigres-com-v1alpha1-multigrescluster
	server.Register(
		"/validate-multigres-com-v1alpha1-multigrescluster",
		&webhook.Admission{
			Handler: handlers.NewMultigresClusterValidator(mgr.GetClient()),
		},
	)

	// -- Validating Webhook (DeploymentTemplate) --
	// Path: /validate-multigres-com-v1alpha1-deploymenttemplate
	server.Register(
		"/validate-multigres-com-v1alpha1-deploymenttemplate",
		&webhook.Admission{
			Handler: handlers.NewDeploymentTemplateValidator(mgr.GetClient()),
		},
	)

	// -- Validating Webhook (Child Resources) --
	// Fallback logic for ValidatingAdmissionPolicy.
	// We register one handler for multiple paths (Cell, Shard, TopoServer, etc.)
	childValidator := &webhook.Admission{Handler: handlers.NewChildResourceValidator()}

	// Paths must match the ValidatingWebhookConfiguration rules you will eventually generate/write
	childResources := []string{"cell", "shard", "toposerver", "tablegroup"}
	for _, res := range childResources {
		path := fmt.Sprintf("/validate-multigres-com-v1alpha1-%s", res)
		server.Register(path, childValidator)
	}

	// 3. Register standard types for built-in defaulting/validation if needed
	// (Only if you were using the simpler Defaulter/Validator interfaces on the types themselves,
	// but we are using custom handlers to keep logic out of the API package).

	return nil
}
