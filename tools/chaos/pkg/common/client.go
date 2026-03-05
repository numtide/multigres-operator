package common

import (
	"fmt"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Clients holds all Kubernetes clients needed by the observer.
type Clients struct {
	// Client is a controller-runtime client that handles both core and CRD types.
	Client client.Client

	// Clientset is the typed Kubernetes client (needed for pod logs).
	Clientset kubernetes.Interface

	// RestConfig is the underlying REST config.
	RestConfig *rest.Config
}

// NewClients builds Kubernetes clients. It tries in-cluster config first,
// falling back to the provided kubeconfig path for local development.
func NewClients(kubeconfigPath string) (*Clients, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		if kubeconfigPath == "" {
			return nil, fmt.Errorf("not running in-cluster and no --kubeconfig provided: %w", err)
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("building kubeconfig from %s: %w", kubeconfigPath, err)
		}
	}

	scheme := runtime.NewScheme()
	if err := multigresv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering multigres scheme: %w", err)
	}

	// Register core k8s types.
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering kubernetes scheme: %w", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating controller-runtime client: %w", err)
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes clientset: %w", err)
	}

	return &Clients{
		Client:     c,
		Clientset:  cs,
		RestConfig: cfg,
	}, nil
}
