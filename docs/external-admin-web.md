# External Admin Web

The Multigres Operator can expose the global multiadmin-web Service for external access, enabling browser-based administration without requiring per-project LoadBalancer Services or manual port forwarding.

## How It Works

Every MultigresCluster with `multiadminWeb` configured creates a **global multiadmin-web Service** (`<cluster>-multiadmin-web`) that selects all multiadmin-web pods. By default, this Service is `ClusterIP` — internal only.

When `spec.externalAdminWeb` is enabled, the operator:

1. Assigns `spec.externalIPs` on the multiadmin-web Service for external reachability
2. Applies user-provided annotations (e.g., for cloud load balancer controllers)
3. Publishes the resolved endpoint to `status.adminWeb.externalEndpoint`
4. Manages an `AdminWebExternalReady` condition with deterministic transitions

The Service type remains `ClusterIP` — external reachability is provided via explicitly assigned external IPs and platform networking, not by switching to `LoadBalancer`.

## Configuration

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: my-cluster
spec:
  multiadminWeb:
    spec:
      replicas: 1
  externalAdminWeb:
    enabled: true
    externalIPs:
      - "198.51.100.20"
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
      service.beta.kubernetes.io/aws-load-balancer-type: "external"
```

### Fields

| Field | Type | Required | Description |
|:---|:---|:---|:---|
| `enabled` | bool | Yes | Enable or disable external admin web exposure |
| `externalIPs` | []string | No | Externally routable IPv4/IPv6 addresses assigned to the Service |
| `annotations` | map | No | Annotations applied to the multiadmin-web Service |

### ExternalIPs

Each entry must be a valid IPv4 or IPv6 address. Maximum 10 entries. Examples:

```yaml
externalIPs:
  - "198.51.100.20"           # IPv4
  - "2001:db8::200"           # IPv6
```

The operator validates IPs at admission time — invalid addresses are rejected.

### Annotations

Annotations are applied directly to the multiadmin-web Service metadata. Maximum 20 entries. Use these for cloud provider integration:

```yaml
# AWS Network Load Balancer
annotations:
  service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
  service.beta.kubernetes.io/aws-load-balancer-type: "external"

# GCP Internal Load Balancer
annotations:
  cloud.google.com/l4-rbs: "enabled"
```

Annotations under the `multigres.com/` prefix are reserved and will be rejected.

## Status and Readiness

### AdminWebExternalReady Condition

The operator manages an `AdminWebExternalReady` condition on the MultigresCluster status:

| Status | Reason | Meaning |
|:---|:---|:---|
| `False` | `AwaitingEndpoint` | No external endpoint provisioned yet (Service not created or no externalIPs/LB ingress) |
| `False` | `NoReadyAdminWeb` | External endpoint assigned but no multiadmin-web pods are ready to serve traffic |
| `True` | `EndpointReady` | External endpoint is serving traffic |

### Admin Web Status

When enabled, `status.adminWeb` reports the resolved endpoint:

```yaml
status:
  adminWeb:
    externalEndpoint: "198.51.100.20"
  conditions:
    - type: AdminWebExternalReady
      status: "True"
      reason: EndpointReady
      message: "External endpoint 198.51.100.20 is serving traffic"
```

When disabled, `status.adminWeb` is `nil` and the condition is removed.

### Endpoint Resolution Priority

The endpoint is resolved in this order:
1. `Service.spec.externalIPs[0]` (preferred — explicitly controlled)
2. `Service.status.loadBalancer.ingress[0].hostname` (fallback — cloud LB)
3. `Service.status.loadBalancer.ingress[0].ip` (fallback — cloud LB)

## Disabling

Set `enabled: false` to revert to internal-only:

```yaml
spec:
  externalAdminWeb:
    enabled: false
```

This removes externalIPs and annotations from the Service, clears `status.adminWeb`, and removes the `AdminWebExternalReady` condition.

## Using with External Gateway

`externalAdminWeb` and `externalGateway` are independent — you can enable either or both:

```yaml
spec:
  externalGateway:
    enabled: true
    externalIPs:
      - "198.51.100.10"
  externalAdminWeb:
    enabled: true
    externalIPs:
      - "198.51.100.20"
```

Each feature manages its own Service, status field, and condition independently. The multigateway Service exposes PostgreSQL traffic on port 15432, while the multiadmin-web Service exposes the HTTP admin UI on port 18100.

## EKS / Cloud Deployment

On AWS EKS, a common pattern is to use externalIPs with an AWS Network Load Balancer:

```yaml
spec:
  externalAdminWeb:
    enabled: true
    externalIPs:
      - "10.0.1.200"  # Pre-allocated Elastic IP
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
      service.beta.kubernetes.io/aws-load-balancer-type: "external"
      service.beta.kubernetes.io/aws-load-balancer-nlb-target-type: "ip"
```

On GKE, use the GCP load balancer annotations instead.

## Webhook Warning

If `externalAdminWeb.enabled: true` is set without any `externalIPs`, the admission webhook emits a warning:

> externalAdminWeb is enabled but no externalIPs specified; endpoint resolution depends on an external load balancer controller provisioning an ingress address on the multiadmin-web Service

This is informational — the feature still works if an external controller (like the AWS Load Balancer Controller) provisions the LB ingress. But without explicit IPs or an LB controller, the endpoint will never be resolved and the condition will remain `AwaitingEndpoint`.
