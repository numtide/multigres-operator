package webhook

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/numtide/multigres-operator/pkg/resolver"
	"github.com/numtide/multigres-operator/pkg/webhook/cert"
	"github.com/numtide/multigres-operator/pkg/webhook/handlers"
)

// Options contains the configuration required to set up the webhook server.
type Options struct {
	Enable             bool
	CertStrategy       string
	CertDir            string
	Namespace          string
	ServiceAccountName string
}

// Setup configures the webhook server.
func Setup(mgr ctrl.Manager, res *resolver.Resolver, opts Options) error {
	if !opts.Enable {
		return nil
	}

	// SAFETY CHECK: Ensure resolver is provided
	if res == nil {
		return fmt.Errorf("webhook setup failed: resolver cannot be nil")
	}

	logger := mgr.GetLogger().WithName("webhook-setup")
	logger.Info("Setting up webhook server", "strategy", opts.CertStrategy)

	// 1. Certificate Management
	if opts.CertStrategy == "self-signed" {
		directClient, err := client.New(mgr.GetConfig(), client.Options{
			Scheme: mgr.GetScheme(),
		})
		if err != nil {
			return fmt.Errorf("failed to create direct client for cert bootstrap: %w", err)
		}

		certMgr := cert.NewManager(directClient, cert.Options{
			Namespace: opts.Namespace,
			// ServiceName is auto-discovered by the manager using the direct client
			CertDir: opts.CertDir,
		})
		if err := certMgr.EnsureCerts(context.Background()); err != nil {
			return fmt.Errorf("failed to bootstrap self-signed certificates: %w", err)
		}
	}

	// 2. Prepare Decoder
	// Since we manually register handlers, we must manually inject the decoder.
	decoder := admission.NewDecoder(mgr.GetScheme())

	// 3. Initialize Handlers & Inject Decoder

	// -- Mutating --
	defaulter := handlers.NewMultigresClusterDefaulter(res)
	if err := defaulter.InjectDecoder(decoder); err != nil {
		return fmt.Errorf("failed to inject decoder into defaulter: %w", err)
	}

	// -- Validating (Cluster) --
	clusterValidator := handlers.NewMultigresClusterValidator(mgr.GetClient())
	if err := clusterValidator.InjectDecoder(decoder); err != nil {
		return fmt.Errorf("failed to inject decoder into cluster validator: %w", err)
	}

	// -- Validating (Templates) --
	coreTplValidator := handlers.NewTemplateValidator(mgr.GetClient(), "CoreTemplate")
	// TemplateValidators don't strictly need a decoder if they only read Req.Name,
	// but it's good practice if they ever read the body.

	cellTplValidator := handlers.NewTemplateValidator(mgr.GetClient(), "CellTemplate")
	shardTplValidator := handlers.NewTemplateValidator(mgr.GetClient(), "ShardTemplate")

	// -- Validating (Child Resources) --
	operatorPrincipal := fmt.Sprintf(
		"system:serviceaccount:%s:%s",
		opts.Namespace,
		opts.ServiceAccountName,
	)
	if opts.ServiceAccountName == "" {
		operatorPrincipal = fmt.Sprintf(
			"system:serviceaccount:%s:multigres-operator",
			opts.Namespace,
		)
	}
	childValidator := handlers.NewChildResourceValidator(operatorPrincipal)
	if err := childValidator.InjectDecoder(decoder); err != nil {
		return fmt.Errorf("failed to inject decoder into child validator: %w", err)
	}

	// 4. Register Webhooks
	server := mgr.GetWebhookServer()

	server.Register(
		"/mutate-multigres-com-v1alpha1-multigrescluster",
		&webhook.Admission{Handler: defaulter},
	)

	server.Register(
		"/validate-multigres-com-v1alpha1-multigrescluster",
		&webhook.Admission{Handler: clusterValidator},
	)

	server.Register(
		"/validate-multigres-com-v1alpha1-coretemplate",
		&webhook.Admission{Handler: coreTplValidator},
	)
	server.Register(
		"/validate-multigres-com-v1alpha1-celltemplate",
		&webhook.Admission{Handler: cellTplValidator},
	)
	server.Register(
		"/validate-multigres-com-v1alpha1-shardtemplate",
		&webhook.Admission{Handler: shardTplValidator},
	)

	childResources := []string{"cell", "shard", "toposerver", "tablegroup"}
	for _, res := range childResources {
		path := fmt.Sprintf("/validate-multigres-com-v1alpha1-%s", res)
		server.Register(path, &webhook.Admission{Handler: childValidator})
	}

	return nil
}
