// Package cert handles the lifecycle management of TLS certificates for the admission webhook server.
//
// It supports two primary modes of operation:
//
//  1. Self-Signed (Auto-Bootstrap & Rotation):
//     The package generates a Root CA and a Server Certificate in-memory, persists them
//     to a Kubernetes Secret, and writes them to the local filesystem for controller-runtime.
//     It also patches the MutatingWebhookConfiguration and ValidatingWebhookConfiguration
//     resources with the generated CA Bundle.
//
//     Crucially, this mode handles AUTOMATIC ROTATION. On startup, it checks the
//     expiration of the certificate stored in the Secret. If the certificate is expired
//     or within the rotation threshold (30 days), it automatically regenerates the
//     artifacts and updates the Secret and WebhookConfigurations.
//
//  2. External (e.g., cert-manager):
//     In this mode, the package expects certificates to be provisioned by an external
//     controller (like cert-manager) and mounted into the container. It simply points
//     the webhook server to the correct directory.
//
// Usage:
//
//	mgr := cert.NewManager(client, options)
//	if err := mgr.EnsureCerts(ctx); err != nil {
//	    // handle error
//	}
package cert
