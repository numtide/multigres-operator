// Package cert handles the lifecycle management of TLS certificates for the admission webhook server.
//
// It supports two primary modes of operation:
//
//  1. Self-Signed (Auto-Bootstrap & Rotation):
//     This package implements a production-grade Split-Secret PKI architecture.
//
//     Architecture:
//     - Root CA: Generated once and stored in 'multigres-operator-ca-secret'.
//     This secret is NEVER mounted to the operator pod to prevent key compromise.
//     - Server Cert: Signed by the Root CA and stored in 'multigres-webhook-certs'.
//     This secret IS mounted to the pod via a Kubelet projected volume.
//
//     Lifecycle:
//     - Bootstrap: On startup, it checks if the secrets exist. If not, it generates them.
//     - Propagation: It waits for the Kubelet to project the secret files to disk before
//     allowing the webhook server to start (preventing "split-brain" race conditions).
//     - Rotation: A background loop checks for expiration hourly. If the server cert
//     is expiring (or the CA changes), it automatically renews the secrets.
//     - Injection: It patches the MutatingWebhookConfiguration and ValidatingWebhookConfiguration
//     resources with the correct CA Bundle using conflict-free server-side patches.
//     - Observability: Emits standard Kubernetes Events for all rotation actions.
//
//  2. External (e.g., cert-manager):
//     In this mode, the package expects certificates to be provisioned by an external
//     controller (like cert-manager) and mounted into the container. It simply points
//     the webhook server to the correct directory.
//
// Usage:
//
//	mgr := cert.NewManager(client, recorder, options)
//	if err := mgr.Bootstrap(ctx); err != nil {
//	    // handle error
//	}
package cert
