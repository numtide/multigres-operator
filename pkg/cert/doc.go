// Package cert provides a generic, consumer-agnostic TLS certificate lifecycle manager
// for Kubernetes operators.
//
// It supports two primary modes of operation:
//
//  1. Self-Signed (Auto-Bootstrap & Rotation):
//     This package implements a production-grade Split-Secret PKI architecture.
//
//     Architecture:
//     - Root CA: Generated once and stored in a Kubernetes Secret (name configured via Options).
//     This secret should NOT be mounted to any pod to prevent key compromise.
//     - Server Cert: Signed by the Root CA and stored in a separate Secret.
//
//     Lifecycle:
//     - Bootstrap: On startup, checks if the secrets exist. If not, generates them.
//     - Rotation: A background loop checks for expiration hourly. If the server cert
//     is expiring (or the CA changes), it automatically renews the secrets.
//     - Hooks: After reconciling PKI, an optional PostReconcileHook is called with the
//     CA bundle. Consumers use this for tasks like patching webhook configurations.
//     - Projection Wait: Optionally waits for the Kubelet to project secret files to disk
//     before returning from Bootstrap (useful for projected volume mounts).
//     - Observability: Emits standard Kubernetes Events for all rotation actions.
//
//  2. External (e.g., cert-manager):
//     In this mode, certificates are provisioned by an external controller and the
//     consumer simply points to the correct directory. The cert package is not used.
//
// Usage:
//
//	mgr := cert.NewManager(client, recorder, cert.Options{
//	    Namespace:        "my-ns",
//	    CASecretName:     "my-ca-secret",
//	    ServerSecretName: "my-server-certs",
//	    ServiceName:      "my-service",
//	})
//	if err := mgr.Bootstrap(ctx); err != nil {
//	    // handle error
//	}
package cert
