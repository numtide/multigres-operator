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
	"context"
	"crypto/tls"
	"flag"
	"os"
	"path/filepath"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
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
	cert "github.com/numtide/multigres-operator/pkg/webhook/cert"
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
	var webhookCertDir string
	var webhookServiceNamespace string
	var webhookServiceAccount string
	var webhookServiceName string

	// Template Default Flags
	var defaultCoreTemplate string
	var defaultCellTemplate string
	var defaultShardTemplate string

	defaultNS := os.Getenv("POD_NAMESPACE")
	if defaultNS == "" {
		defaultNS = "multigres-system"
	}

	defaultSA := os.Getenv("POD_SERVICE_ACCOUNT")
	if defaultSA == "" {
		defaultSA = "multigres-operator-controller-manager"
	}

	// General Flags
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")

	// Webhook Flag Configuration
	flag.BoolVar(&webhookEnabled, "webhook-enable", true, "Enable the admission webhook server")
	// CHANGE: Aligned with config/manager/manager.yaml volumeMount
	flag.StringVar(&webhookCertDir, "webhook-cert-dir", "/var/run/secrets/webhook", "Directory to store/read webhook certificates")
	flag.StringVar(&webhookServiceNamespace, "webhook-service-namespace", defaultNS, "Namespace where the webhook service resides")
	flag.StringVar(&webhookServiceAccount, "webhook-service-account", defaultSA, "Service Account name of the operator")
	flag.StringVar(&webhookServiceName, "webhook-service-name", "multigres-operator-webhook-service", "Name of the Kubernetes Service for the webhook")

	// Template Defaults
	flag.StringVar(&defaultCoreTemplate, "default-core-template", "default", "Default CoreTemplate name")
	flag.StringVar(&defaultCellTemplate, "default-cell-template", "default", "Default CellTemplate name")
	flag.StringVar(&defaultShardTemplate, "default-shard-template", "default", "Default ShardTemplate name")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// 1. Auto-Detect Certificate Strategy
	// If the cert files already exist (e.g. mounted by Cert-Manager), we skip internal generation.
	useInternalCerts := false
	if webhookEnabled {
		if !certsExist(webhookCertDir) {
			setupLog.Info("webhook certificates not found on disk; enabling internal certificate rotation")
			useInternalCerts = true
		} else {
			setupLog.Info("webhook certificates found on disk; using external certificate management")
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "multigres-operator.multigres.com",
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port:    9443,
			CertDir: webhookCertDir,
			TLSOpts: tlsOpts,
		}),
		Client: client.Options{
			// Disable caching for resources we need during bootstrap/cert rotation
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.Secret{},
					&appsv1.Deployment{},
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

	// 2. Setup Internal Certificate Rotation (If enabled)
	if webhookEnabled && useInternalCerts {
		// Use a temporary client for bootstrap since mgr.Client isn't started yet
		tmpClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
		if err != nil {
			setupLog.Error(err, "failed to create bootstrap client")
			os.Exit(1)
		}

		rotator := cert.NewManager(tmpClient, cert.Options{
			Namespace:          webhookServiceNamespace,
			ServiceName:        webhookServiceName,
			CertDir:            webhookCertDir,
			OperatorDeployment: "multigres-operator-controller-manager", // Standard Kubebuilder deployment name
		})

		// Bootstrap immediately to unblock Webhook Server start
		if err := rotator.Bootstrap(context.Background()); err != nil {
			setupLog.Error(err, "failed to bootstrap certificates")
			os.Exit(1)
		}

		// Register rotator as a background runnable (forever rotation)
		// We switch the client to the Manager's client for the long-running process
		rotator.Client = mgr.GetClient()
		if err := mgr.Add(rotator); err != nil {
			setupLog.Error(err, "unable to add cert rotator to manager")
			os.Exit(1)
		}
	}

	// 3. Initialize Resolver & Controllers
	globalResolver := resolver.NewResolver(
		mgr.GetClient(),
		webhookServiceNamespace,
		multigresv1alpha1.TemplateDefaults{
			CoreTemplate:  defaultCoreTemplate,
			CellTemplate:  defaultCellTemplate,
			ShardTemplate: defaultShardTemplate,
		},
	)

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

	// 4. Register Webhook Handlers
	if webhookEnabled {
		if err := multigreswebhook.Setup(mgr, globalResolver, multigreswebhook.Options{
			Namespace:          webhookServiceNamespace,
			ServiceAccountName: webhookServiceAccount,
		}); err != nil {
			setupLog.Error(err, "unable to set up webhook")
			os.Exit(1)
		}
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

func certsExist(dir string) bool {
	_, errCrt := os.Stat(filepath.Join(dir, "tls.crt"))
	_, errKey := os.Stat(filepath.Join(dir, "tls.key"))
	return !os.IsNotExist(errCrt) && !os.IsNotExist(errKey)
}
