# External Gateway

The Multigres Operator can expose the global multigateway Service for external access, enabling DNS automation and external client connectivity without requiring per-project LoadBalancer Services.

## How It Works

Every MultigresCluster creates a **global multigateway Service** (`<cluster>-multigateway`) that selects all multigateway pods across all cells. By default, this Service is `ClusterIP` — internal only.

When `spec.externalGateway` is enabled, the operator:

1. Assigns `spec.externalIPs` on the global Service for external reachability
2. Applies user-provided annotations (e.g., for cloud load balancer controllers)
3. Publishes the resolved endpoint to `status.gateway.externalEndpoint`
4. Manages a `GatewayExternalReady` condition with deterministic transitions

The Service type remains `ClusterIP` — external reachability is provided via explicitly assigned external IPs and platform networking, not by switching to `LoadBalancer`.

## Configuration

```yaml
apiVersion: multigres.com/v1alpha1
kind: MultigresCluster
metadata:
  name: my-cluster
spec:
  externalGateway:
    enabled: true
    externalIPs:
      - "198.51.100.10"
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
      service.beta.kubernetes.io/aws-load-balancer-type: "external"
```

### Fields

| Field | Type | Required | Description |
|:---|:---|:---|:---|
| `enabled` | bool | Yes | Enable or disable external gateway exposure |
| `externalIPs` | []string | No | Externally routable IPv4/IPv6 addresses assigned to the Service |
| `annotations` | map | No | Annotations applied to the global multigateway Service |

### ExternalIPs

Each entry must be a valid IPv4 or IPv6 address. Examples:

```yaml
externalIPs:
  - "198.51.100.10"           # IPv4
  - "2001:db8::100"           # IPv6
```

The operator validates IPs at admission time — invalid addresses are rejected.

### Annotations

Annotations are applied directly to the global multigateway Service metadata. Use these for cloud provider integration:

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

### GatewayExternalReady Condition

The operator manages a `GatewayExternalReady` condition on the MultigresCluster status:

| Status | Reason | Meaning |
|:---|:---|:---|
| `False` | `AwaitingEndpoint` | No external endpoint provisioned yet (Service not created or no externalIPs/LB ingress) |
| `False` | `NoReadyGateways` | External endpoint assigned but no multigateway pods are ready to serve traffic |
| `True` | `EndpointReady` | External endpoint is serving traffic |

### Gateway Status

When enabled, `status.gateway` reports the resolved endpoint:

```yaml
status:
  gateway:
    externalEndpoint: "198.51.100.10"
  conditions:
    - type: GatewayExternalReady
      status: "True"
      reason: EndpointReady
      message: "External endpoint 198.51.100.10 is serving traffic"
```

When disabled, `status.gateway` is `nil` and the condition is removed.

### Endpoint Resolution Priority

The endpoint is resolved in this order:
1. `Service.spec.externalIPs[0]` (preferred — explicitly controlled)
2. `Service.status.loadBalancer.ingress[0].hostname` (fallback — cloud LB)
3. `Service.status.loadBalancer.ingress[0].ip` (fallback — cloud LB)

## Disabling

Set `enabled: false` to revert to internal-only:

```yaml
spec:
  externalGateway:
    enabled: false
```

This removes externalIPs and annotations from the Service, clears `status.gateway`, and removes the `GatewayExternalReady` condition.

## DNS Automation

The `GatewayExternalReady` condition and `status.gateway.externalEndpoint` provide a stable API contract for DNS automation. A DNS controller can watch the MultigresCluster status and:

1. Wait for `GatewayExternalReady` to be `True`
2. Read `status.gateway.externalEndpoint`
3. Create/update a DNS record pointing to the endpoint
4. Remove the DNS record when the condition is removed (gateway disabled)

## EKS / Cloud Deployment

On AWS EKS, a common pattern is to use externalIPs with an AWS Network Load Balancer. The annotations configure the AWS Load Balancer Controller to provision the NLB:

```yaml
spec:
  externalGateway:
    enabled: true
    externalIPs:
      - "10.0.1.100"  # Pre-allocated Elastic IP
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
      service.beta.kubernetes.io/aws-load-balancer-type: "external"
      service.beta.kubernetes.io/aws-load-balancer-nlb-target-type: "ip"
```

On GKE, use the GCP load balancer annotations instead.

## Webhook Warning

If `externalGateway.enabled: true` is set without any `externalIPs`, the admission webhook emits a warning:

> externalGateway is enabled but no externalIPs specified; endpoint resolution depends on an external load balancer controller provisioning an ingress address on the global multigateway Service

This is informational — the feature still works if an external controller (like the AWS Load Balancer Controller) provisions the LB ingress. But without explicit IPs or an LB controller, the endpoint will never be resolved and the condition will remain `AwaitingEndpoint`.
