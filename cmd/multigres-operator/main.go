/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	multigresclustercontroller "github.com/numtide/multigres-operator/pkg/cluster-handler/controller/multigrescluster"
	tablegroupcontroller "github.com/numtide/multigres-operator/pkg/cluster-handler/controller/tablegroup"
	cellcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/cell"
	shardcontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/shard"
	toposervercontroller "github.com/numtide/multigres-operator/pkg/resource-handler/controller/toposerver"

	"github.com/numtide/multigres-operator/pkg/resolver"
	multigreswebhook "github.com/numtide/multigres-operator/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(multigresv1alpha1.AddToScheme(scheme))
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)

	// Webhook Flags
	var webhookEnabled bool
	var webhookCertStrategy string
	var webhookCertDir string
	var webhookServiceName string
	var webhookServiceNamespace string
	var webhookServiceAccount string

	// Template Default Flags
	var defaultCoreTemplate string
	var defaultCellTemplate string
	var defaultShardTemplate string

	// General Flags
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	// Webhook Flag Configuration
	flag.BoolVar(&webhookEnabled, "webhook-enable", true, "Enable the admission webhook server")
	flag.StringVar(&webhookCertStrategy, "webhook-cert-strategy", "self-signed", "Certificate management strategy: 'self-signed', 'cert-manager', or 'external'")
	flag.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory to store/read webhook certificates")
	flag.StringVar(&webhookServiceName, "webhook-service-name", "multigres-operator-webhook", "Name of the Kubernetes Service for the webhook")
	flag.StringVar(&webhookServiceNamespace, "webhook-service-namespace", "multigres-system", "Namespace where the webhook service resides")
	flag.StringVar(&webhookServiceAccount, "webhook-service-account", "multigres-operator", "Service Account name of the operator (exempt from child resource validation)")

	// Template Defaults Configuration
	flag.StringVar(&defaultCoreTemplate, "default-core-template", "default", "Default CoreTemplate name to use if not specified in CR")
	flag.StringVar(&defaultCellTemplate, "default-cell-template", "default", "Default CellTemplate name to use if not specified in CR")
	flag.StringVar(&defaultShardTemplate, "default-shard-template", "default", "Default ShardTemplate name to use if not specified in CR")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more info see:
	// https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "multigres-operator.multigres.com",
		// Configure the Webhook Server with our flags
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port:    9443,
			CertDir: webhookCertDir,
			TLSOpts: tlsOpts,
		}),
		// CRITICAL: Disable caching for resources used during Webhook Setup (Cert Generation).
		// Since webhook.Setup() runs BEFORE mgr.Start(), the cache is not yet active.
		// By disabling cache for these types, the manager's Client will make direct API calls,
		// allowing EnsureCerts to read/write Secrets and WebhookConfigs during bootstrapping.
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.Secret{},
					&admissionregistrationv1.MutatingWebhookConfiguration{},
					&admissionregistrationv1.ValidatingWebhookConfiguration{},
				},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// 1. Initialize the Global Resolver
	// This resolver carries the cluster-wide defaults provided via CLI flags.
	// It is shared by the Webhook (for defaulting) and can be used by controllers.
	globalResolver := resolver.NewResolver(
		mgr.GetClient(),
		webhookServiceNamespace, // Namespace where "default" templates are expected to exist
		multigresv1alpha1.TemplateDefaults{
			CoreTemplate:  defaultCoreTemplate,
			CellTemplate:  defaultCellTemplate,
			ShardTemplate: defaultShardTemplate,
		},
	)

	// 2. Setup Controllers
	if err = (&multigresclustercontroller.MultigresClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MultigresCluster")
		os.Exit(1)
	}

	if err = (&tablegroupcontroller.TableGroupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TableGroup")
		os.Exit(1)
	}

	if err = (&cellcontroller.CellReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cell")
		os.Exit(1)
	}

	if err = (&toposervercontroller.TopoServerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TopoServer")
		os.Exit(1)
	}

	if err = (&shardcontroller.ShardReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Shard")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	// 3. Setup Webhook (Admission Control)
	if err := multigreswebhook.Setup(mgr, globalResolver, multigreswebhook.Options{
		Enable:             webhookEnabled,
		CertStrategy:       webhookCertStrategy,
		CertDir:            webhookCertDir,
		Namespace:          webhookServiceNamespace,
		ServiceName:        webhookServiceName,
		ServiceAccountName: webhookServiceAccount,
	}); err != nil {
		setupLog.Error(err, "unable to set up webhook")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
