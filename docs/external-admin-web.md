# External Admin Web

The Multigres Operator can expose the global multiadmin-web Service for external access, enabling browser-based administration without requiring per-project LoadBalancer Services or manual port forwarding.

## How It Works

Every MultigresCluster with `multiadminWeb` configured creates a **global multiadmin-web Service** (`<cluster>-multiadmin-web`) that selects all multiadmin-web pods. By default, this Service is `ClusterIP` — internal only.

When `spec.externalAdminWeb` is enabled, the operator:

1. Assigns `spec.externalIPs` on the multiadmin-web Service for external reachability
2. Applies user-provided annotations to the Service metadata
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
```

### Fields

| Field | Type | Required | Description |
|:---|:---|:---|:---|
| `enabled` | bool | Yes | Enable or disable external admin web exposure |
| `externalIPs` | []string | Yes | Externally routable IPv4/IPv6 addresses assigned to the Service |
| `annotations` | map | No | Annotations applied to the multiadmin-web Service |

### ExternalIPs

Each entry must be a valid IPv4 or IPv6 address. Maximum 10 entries. Examples:

```yaml
externalIPs:
  - "198.51.100.20"           # IPv4
  - "2001:db8::200"           # IPv6
```

The operator validates IPs at admission time — invalid addresses are rejected.

> **How `externalIPs` works:** When `spec.externalIPs` is set on a Service, `kube-proxy` on every node creates iptables/IPVS rules to NAT traffic arriving with that destination IP to the Service's backing pods. However, Kubernetes does not attract traffic to the nodes — the cluster administrator must ensure the external IP is routed to one or more cluster nodes via platform networking (e.g., Elastic IP association on AWS, MetalLB on bare metal, or manual routing).

### Annotations

Annotations are applied directly to the multiadmin-web Service metadata. Maximum 20 entries. This is a generic pass-through for any metadata the user wants on the Service (e.g., monitoring labels, service mesh configuration, organizational tags).

> **Note:** Annotations under the `multigres.com/` prefix are reserved and will be rejected. Cloud load balancer annotations (e.g., `service.beta.kubernetes.io/aws-load-balancer-*`) have no effect on a `ClusterIP` Service — cloud LB controllers only act on `type: LoadBalancer` Services.

## Status and Readiness

### AdminWebExternalReady Condition

The operator manages an `AdminWebExternalReady` condition on the MultigresCluster status:

| Status | Reason | Meaning |
|:---|:---|:---|
| `False` | `AwaitingEndpoint` | No external endpoint provisioned yet (no externalIPs configured) |
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

### Endpoint Resolution

The endpoint is resolved from `Service.spec.externalIPs[0]`.

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

On AWS EKS, a common pattern is to use `externalIPs` with a pre-allocated Elastic IP:

```yaml
spec:
  externalAdminWeb:
    enabled: true
    externalIPs:
      - "10.0.1.200"  # Pre-allocated Elastic IP
```

The administrator must ensure the Elastic IP is routed to the cluster nodes (e.g., associated with an ENI on a node, or routed via a separately managed NLB/target group). The operator only sets `spec.externalIPs` on the Service — it does not provision any cloud infrastructure.

On bare metal, tools like MetalLB can advertise the external IP via ARP (L2) or BGP (L3) to attract traffic to the correct nodes.

## Webhook Warning

If `externalAdminWeb.enabled: true` is set without any `externalIPs`, the admission webhook emits a warning:

> externalAdminWeb is enabled but no externalIPs specified; the external endpoint will not be resolved and the AdminWebExternalReady condition will remain AwaitingEndpoint

`externalIPs` is the only mechanism for endpoint resolution on a `ClusterIP` Service.
