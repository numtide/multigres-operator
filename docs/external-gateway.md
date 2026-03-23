# External Gateway

The Multigres Operator can expose the global multigateway Service for external access, enabling DNS automation and external client connectivity without requiring per-project LoadBalancer Services.

## How It Works

Every MultigresCluster creates a **global multigateway Service** (`<cluster>-multigateway`) that selects all multigateway pods across all cells. By default, this Service is `ClusterIP` — internal only.

When `spec.externalGateway` is enabled, the operator:

1. Assigns `spec.externalIPs` on the global Service for external reachability
2. Applies user-provided annotations to the Service metadata
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
```

### Fields

| Field | Type | Required | Description |
|:---|:---|:---|:---|
| `enabled` | bool | Yes | Enable or disable external gateway exposure |
| `externalIPs` | []string | Yes | Externally routable IPv4/IPv6 addresses assigned to the Service |
| `annotations` | map | No | Annotations applied to the global multigateway Service |

### ExternalIPs

Each entry must be a valid IPv4 or IPv6 address. Examples:

```yaml
externalIPs:
  - "198.51.100.10"           # IPv4
  - "2001:db8::100"           # IPv6
```

The operator validates IPs at admission time — invalid addresses are rejected.

> **How `externalIPs` works:** When `spec.externalIPs` is set on a Service, `kube-proxy` on every node creates iptables/IPVS rules to NAT traffic arriving with that destination IP to the Service's backing pods. However, Kubernetes does not attract traffic to the nodes — the cluster administrator must ensure the external IP is routed to one or more cluster nodes via platform networking (e.g., Elastic IP association on AWS, MetalLB on bare metal, or manual routing).

### Annotations

Annotations are applied directly to the global multigateway Service metadata. This is a generic pass-through for any metadata the user wants on the Service (e.g., monitoring labels, service mesh configuration, organizational tags).

> **Note:** Annotations under the `multigres.com/` prefix are reserved and will be rejected. Cloud load balancer annotations (e.g., `service.beta.kubernetes.io/aws-load-balancer-*`) have no effect on a `ClusterIP` Service — cloud LB controllers only act on `type: LoadBalancer` Services.

## Status and Readiness

### GatewayExternalReady Condition

The operator manages a `GatewayExternalReady` condition on the MultigresCluster status:

| Status | Reason | Meaning |
|:---|:---|:---|
| `False` | `AwaitingEndpoint` | No external endpoint provisioned yet (no externalIPs configured) |
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

### Endpoint Resolution

The endpoint is resolved from `Service.spec.externalIPs[0]`.

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

On AWS EKS, a common pattern is to use `externalIPs` with a pre-allocated Elastic IP:

```yaml
spec:
  externalGateway:
    enabled: true
    externalIPs:
      - "10.0.1.100"  # Pre-allocated Elastic IP
```

The administrator must ensure the Elastic IP is routed to the cluster nodes (e.g., associated with an ENI on a node, or routed via a separately managed NLB/target group). The operator only sets `spec.externalIPs` on the Service — it does not provision any cloud infrastructure.

On bare metal, tools like MetalLB can advertise the external IP via ARP (L2) or BGP (L3) to attract traffic to the correct nodes.

## Webhook Warning

If `externalGateway.enabled: true` is set without any `externalIPs`, the admission webhook emits a warning:

> externalGateway is enabled but no externalIPs specified; the external endpoint will not be resolved and the GatewayExternalReady condition will remain AwaitingEndpoint

`externalIPs` is the only mechanism for endpoint resolution on a `ClusterIP` Service.
