// Package webhook provides the entry point for the Multigres Operator's admission control layer.
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
	// ServiceAccountName is the name of the operator's service account.
	// Used to exempt the operator from child resource validation blocks.
	ServiceAccountName string
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
	server.Register(
		"/mutate-multigres-com-v1alpha1-multigrescluster",
		&webhook.Admission{
			Handler: handlers.NewMultigresClusterDefaulter(res),
		},
	)

	// -- Validating Webhook (MultigresCluster) --
	server.Register(
		"/validate-multigres-com-v1alpha1-multigrescluster",
		&webhook.Admission{
			Handler: handlers.NewMultigresClusterValidator(mgr.GetClient()),
		},
	)

	// -- Validating Webhook (Templates - In-Use Protection) --
	server.Register(
		"/validate-multigres-com-v1alpha1-coretemplate",
		&webhook.Admission{
			Handler: handlers.NewTemplateValidator(mgr.GetClient(), "CoreTemplate"),
		},
	)
	server.Register(
		"/validate-multigres-com-v1alpha1-celltemplate",
		&webhook.Admission{
			Handler: handlers.NewTemplateValidator(mgr.GetClient(), "CellTemplate"),
		},
	)
	server.Register(
		"/validate-multigres-com-v1alpha1-shardtemplate",
		&webhook.Admission{
			Handler: handlers.NewTemplateValidator(mgr.GetClient(), "ShardTemplate"),
		},
	)

	// -- Validating Webhook (Child Resources) --
	// Construct the validator with the operator's service account as the exempt principal.
	operatorPrincipal := fmt.Sprintf("system:serviceaccount:%s:%s", opts.Namespace, opts.ServiceAccountName)
	if opts.ServiceAccountName == "" {
		// Fallback defaults if not provided
		operatorPrincipal = fmt.Sprintf("system:serviceaccount:%s:multigres-operator", opts.Namespace)
	}

	childValidator := &webhook.Admission{
		Handler: handlers.NewChildResourceValidator(operatorPrincipal),
	}

	// Paths must match the ValidatingWebhookConfiguration
	childResources := []string{"cell", "shard", "toposerver", "tablegroup"}
	for _, res := range childResources {
		path := fmt.Sprintf("/validate-multigres-com-v1alpha1-%s", res)
		server.Register(path, childValidator)
	}

	return nil
}
