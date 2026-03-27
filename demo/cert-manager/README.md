# Cert-Manager Integration Demo

This demo shows how to deploy the Multigres Operator with [cert-manager](https://cert-manager.io/) for external certificate management instead of the operator's built-in self-signed certificates.

## When to Use This

By default, the operator manages its own TLS certificates automatically (see [Webhook & Certificate Management](../../README.md#webhook--certificate-management)). Use cert-manager when:

- Your organization requires centralized certificate management
- You need certificates signed by a specific CA
- You want cert-manager to handle certificate rotation

## Prerequisites

- A running Kubernetes cluster (or Kind for local testing)
- `kubectl` installed

## Steps

### 1. Install cert-manager

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.17.2/cert-manager.yaml
kubectl wait --for=condition=Available deployment --all -n cert-manager --timeout=120s
```

### 2. Install the operator with the cert-manager overlay

```bash
kubectl apply --server-side -f \
  https://github.com/multigres/multigres-operator/releases/latest/download/install-certmanager.yaml
```

This overlay:
1. Creates a `Certificate` and `ClusterIssuer` resource for cert-manager to manage.
2. Mounts the cert-manager-provisioned secret to `/var/run/secrets/webhook` so certificates exist on disk at startup.

### 3. Verify external certificate mode

```bash
kubectl logs -n multigres-operator deployment/multigres-operator-controller-manager | grep "webhook certificates"
```

**Expected output:**
```
webhook certificates found on disk; using external certificate management
```

The operator automatically detected that certificates were already present on disk and skipped internal rotation.

### Local Development (Kind)

For local testing with Kind:

```bash
make kind-deploy-certmanager
kubectl wait --for=condition=Available deployment/multigres-operator-controller-manager -n multigres-operator --timeout=180s
```

## How It Works

The operator **automatically detects** the certificate management strategy on startup:
- If certificates already exist on disk and the operator did not previously manage them (no cert-strategy annotation), it assumes an external provider (e.g. cert-manager) and skips internal rotation.
- If no certificates exist on disk, or the operator previously annotated the ValidatingWebhookConfiguration, internal certificate rotation is enabled.

No flags or manual configuration are needed — the detection is fully automatic.
