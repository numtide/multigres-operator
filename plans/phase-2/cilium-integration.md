---
title: Cilium Integration for Multicluster Multigres Deployments
state: draft
tags: [network, multicluster, cilium, infrastructure]
---

# Summary

Enable Multigres operator to deploy clusters that work seamlessly with Cilium's multicluster networking capabilities, ensuring east-west traffic between clusters works correctly and that pod placement (via affinity rules) aligns with Cilium's cluster mesh topology requirements.

# Motivation

Clients are deploying Multigres across multiple Kubernetes clusters with Cilium for:
- **East-west multicluster networking**: Services in one cluster need to reach services in another
- **Advanced networking**: Cilium's service mesh, network policies, and observability
- **Complex topologies**: Multi-region, multi-zone deployments with specific latency and compliance requirements

The operator must ensure that:
1. Multigres components can discover and communicate across cluster boundaries
2. Pod affinity/anti-affinity rules respect Cilium's cluster mesh topology
3. Network policies don't inadvertently block required Multigres traffic
4. Service annotations are compatible with Cilium's service mesh features

## Goals

1. **Validate Multicluster Connectivity**: Ensure etcd, MultiGateway, MultiOrch, and MultiPooler can communicate across Cilium cluster mesh
2. **Align Affinity with Topology**: Provide guidance and examples for setting `affinity`, `nodeSelector`, and `topologySpreadConstraints` that work with Cilium's cluster topology
3. **Service Mesh Compatibility**: Ensure Multigres Services work with Cilium service mesh (L7 policies, observability)
4. **Documentation**: Provide deployment examples for common multicluster scenarios
5. **Testing**: Validate operator deployments in Cilium-enabled multicluster environments

## Non-Goals

1. **Cilium Installation/Management**: Operator does not install or configure Cilium itself
2. **Custom Cilium Policies**: Operator won't generate Cilium-specific NetworkPolicies or CiliumNetworkPolicies (users manage these)
3. **Cilium Feature Development**: Not extending Cilium capabilities, only ensuring compatibility
4. **Non-Cilium CNIs**: This document focuses on Cilium; other CNIs handled separately

# Proposal

## Core Strategy

The operator already exposes standard Kubernetes scheduling primitives (`affinity`, `nodeSelector`, `topologySpreadConstraints`, `tolerations`) in all component CRDs. These primitives are sufficient for Cilium integration - no API changes required.

**Integration approach**:
1. **Service Annotations**: Support Cilium service mesh annotations in `serviceAnnotations` fields
2. **Pod Labels**: Allow users to set pod labels for Cilium network policy matching
3. **Documentation**: Provide Cilium-specific deployment examples and best practices
4. **Validation**: Test multicluster scenarios in CI/CD with Cilium cluster mesh

## Multicluster Architecture Patterns

### Pattern 1: Shared Etcd, Distributed Gateways

```
Cluster A (us-east-1)          Cluster B (eu-west-1)
┌─────────────────────┐        ┌─────────────────────┐
│ Etcd (3 replicas)   │◄──────►│ MultiGateway        │
│ MultiGateway        │        │ MultiPooler         │
│ MultiOrch           │        └─────────────────────┘
│ MultiPooler         │
└─────────────────────┘
         ▲
         │ Cilium Cluster Mesh
         ▼
```

**Affinity Strategy**:
- Etcd pods use `podAntiAffinity` to spread across zones within Cluster A
- MultiGateway/MultiPooler in Cluster B use `nodeSelector` to pin to eu-west-1
- Services annotated with `io.cilium/global-service: "true"` for cross-cluster discovery

### Pattern 2: Federated Etcd Across Clusters

```
Cluster A                      Cluster B
┌─────────────────────┐        ┌─────────────────────┐
│ Etcd (replicas 2)   │◄──────►│ Etcd (replicas 1)   │
│ MultiGateway        │        │ MultiGateway        │
└─────────────────────┘        └─────────────────────┘
```

**Affinity Strategy**:
- Etcd members in each cluster use `topologySpreadConstraints` with `topology.kubernetes.io/zone`
- Cilium ClusterMesh enables etcd peer discovery across clusters
- `podAntiAffinity` prevents multiple etcd members on same node

### Pattern 3: Regional Isolation with Global Gateway

```
Cluster A (Region 1)           Cluster B (Region 2)
┌─────────────────────┐        ┌─────────────────────┐
│ Etcd                │        │ Etcd                │
│ MultiPooler         │        │ MultiPooler         │
└─────────────────────┘        └─────────────────────┘
         ▲                              ▲
         └──────────┬───────────────────┘
                    │
         Global Load Balancer Cluster
         ┌─────────────────────┐
         │ MultiGateway (HA)   │
         └─────────────────────┘
```

**Affinity Strategy**:
- MultiGateway uses `nodeAffinity` to schedule on dedicated gateway nodes
- MultiPooler uses `podAntiAffinity` to avoid co-location
- Cilium's `io.cilium/lb-ipam-ips` annotation for consistent LB addressing

# Design Details

## Service Annotations for Cilium

Multigres components should support Cilium-specific service annotations. The existing `serviceAnnotations` field in MultiGatewaySpec already supports this.

**Recommended Cilium Annotations**:

```yaml
apiVersion: multigres.io/v1alpha1
kind: MultiGateway
metadata:
  name: global-gateway
spec:
  serviceAnnotations:
    # Global service for cross-cluster access
    io.cilium/global-service: "true"
    # L7 traffic management
    io.cilium/lb-ipam-ips: "10.0.0.100"
    # Shared backend pools across clusters
    io.cilium/service-affinity: "remote"
  serviceType: LoadBalancer
```

## Pod Labels and Network Policies

Users should be able to apply pod labels for Cilium network policy matching:

```yaml
apiVersion: multigres.io/v1alpha1
kind: Etcd
metadata:
  name: my-etcd
spec:
  podLabels:
    # Cilium network policy selectors
    network-zone: "trusted"
    multigres-component: "etcd"
  podAnnotations:
    # Cilium policy enforcement mode
    policy.cilium.io/proxy-visibility: "<Egress/53/UDP/DNS>,<Egress/80/TCP/HTTP>"
```

## Affinity Rules for Multicluster Topologies

### Cross-Cluster Etcd with High Availability

```yaml
apiVersion: multigres.io/v1alpha1
kind: Etcd
metadata:
  name: federated-etcd
spec:
  replicas: 5
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/component
            operator: In
            values:
            - etcd
        # Spread across both topology zones AND clusters
        topologyKey: topology.kubernetes.io/zone
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app.kubernetes.io/component: etcd
  - maxSkew: 2
    # Cilium adds this label to nodes in cluster mesh
    topologyKey: topology.cilium.io/cluster
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        app.kubernetes.io/component: etcd
```

### Regional MultiPooler Placement

```yaml
apiVersion: multigres.io/v1alpha1
kind: MultiPooler
metadata:
  name: regional-pooler
spec:
  replicas: 3
  nodeSelector:
    topology.kubernetes.io/region: "us-east-1"
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/component: multipooler
          topologyKey: kubernetes.io/hostname
  tolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "database"
    effect: "NoSchedule"
```

## DNS and Service Discovery

**Cilium ClusterMesh DNS Requirements**:
- Etcd pods must be able to resolve peers across clusters
- Use Cilium's global service discovery or explicit FQDNs

**Etcd Peer URLs**:
```yaml
# In cluster A, etcd member 0:
--initial-cluster=\
  etcd-0=https://etcd-0.etcd-headless.default.svc.cluster.local:2380,\
  etcd-1=https://etcd-1.etcd-headless.cluster-b.mesh.cilium.io:2380
```

**Operator Consideration**: The operator should support an optional `peerURLs` or `externalPeers` field for multicluster etcd bootstrapping (future enhancement).

## Network Policies

**Default Policy Approach**: The operator does NOT create NetworkPolicies by default. Users must create Cilium NetworkPolicies manually.

**Example Cilium NetworkPolicy for Etcd**:
```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: etcd-multicluster
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/component: etcd
  ingress:
  - fromEndpoints:
    - matchLabels:
        app.kubernetes.io/component: etcd
    toPorts:
    - ports:
      - port: "2379"  # client
        protocol: TCP
      - port: "2380"  # peer
        protocol: TCP
  # Allow cross-cluster traffic
  - fromCIDR:
    - 10.0.0.0/8  # Cluster mesh CIDR
    toPorts:
    - ports:
      - port: "2379"
      - port: "2380"
```

## Observability and Metrics

Cilium provides Hubble for network observability. Multigres components should be discoverable:

**Pod Annotations for Hubble**:
```yaml
spec:
  podAnnotations:
    # Enable Hubble flow visibility
    hubble.cilium.io/visibility: "enabled"
    # Prometheus metrics scraping (if using Cilium service mesh metrics)
    prometheus.io/scrape: "true"
    prometheus.io/port: "9090"
```

## Test Plan

### Unit Tests
- Validate that existing affinity/topology fields accept Cilium-specific topology keys
- Ensure service annotations are properly propagated to generated Service manifests

### Integration Tests
1. **Single Cluster with Cilium**: Deploy operator in kind with Cilium CNI
2. **Two-Cluster Mesh**: Deploy etcd in cluster A, MultiGateway in cluster B, verify connectivity
3. **Network Policy Validation**: Apply restrictive CiliumNetworkPolicy, ensure operator components still function
4. **Service Mesh Mode**: Enable Cilium service mesh, verify L7 observability and policies don't break traffic

### Manual Validation Scenarios
1. Deploy Multigres with etcd spread across 3 clusters
2. Verify `kubectl get svc` shows global services with Cilium annotations
3. Test failover: kill etcd pod in one cluster, verify quorum maintained via cluster mesh
4. Validate Hubble UI shows cross-cluster Multigres traffic flows

## Graduation Criteria

### MVP (Initial Release)
- [ ] Documentation with Cilium deployment examples
- [ ] Basic multicluster test (2 clusters, shared etcd)
- [ ] Service annotation support validated

### Production-Ready
- [ ] CI/CD pipeline includes Cilium multicluster tests
- [ ] Performance benchmarks for cross-cluster latency
- [ ] Advanced examples (regional isolation, federated etcd)

### Stable
- [ ] Production deployments validated with Cilium
- [ ] Network policy reference architecture documented
- [ ] Troubleshooting guide for common Cilium issues

## Upgrade / Downgrade Strategy

**No operator changes required** - integration is via existing Kubernetes primitives and annotations.

**User migration path**:
1. Existing deployments continue to work
2. Users opt-in to Cilium features by adding annotations/labels
3. Gradual rollout: update one component CR at a time with Cilium-specific configs

## Version Skew Strategy

**Cilium Versions**: Test against Cilium 1.18 (stable version with mature cluster mesh).

**Kubernetes Versions**: Support Kubernetes 1.32+ (standard operator support range).

**Skew Scenarios**:
- Different Cilium minor versions across clusters should work (Cilium ClusterMesh is version-tolerant within same major version)
- Operator version doesn't affect network layer - no skew issues

# Implementation History

- 2025-10-16: Initial draft created based on client multicluster requirements

# Drawbacks

1. **Complexity**: Multicluster deployments are inherently complex; troubleshooting network issues requires Cilium expertise
2. **Testing Burden**: CI/CD must maintain multicluster test infrastructure (expensive)
3. **Documentation Maintenance**: As Cilium evolves, examples may need updates
4. **No Abstraction**: Operator doesn't abstract Cilium details - users must understand both technologies

# Alternatives

## Alternative 1: Custom Cilium CRD Support

**Approach**: Operator creates CiliumNetworkPolicy resources automatically.

**Rejected because**:
- Opinionated networking is anti-pattern for infrastructure operators
- Different users have different security requirements
- Would require deep Cilium version coupling

## Alternative 2: MultiCluster CRD

**Approach**: Create a new `MultiClusterMultigres` CRD that abstracts cross-cluster deployment.

**Rejected because**:
- Adds significant API complexity
- Users already have tools (GitOps, Helm) for multi-cluster orchestration
- Operator should focus on single-cluster resource management

## Alternative 3: Cilium CNI Autodetection

**Approach**: Operator detects Cilium CNI and automatically applies best-practice annotations.

**Rejected because**:
- Magic behavior is confusing and hard to debug
- Different deployments need different configurations
- Explicit is better than implicit for production systems

# Infrastructure Needed

## Development Environment
- Kind clusters (3x) with Cilium 1.18 CNI installed
- Cilium CLI for cluster mesh setup
- Hubble UI for network observability validation

## CI/CD
- GitHub Actions workflow for multicluster tests
- Infrastructure to spin up multiple clusters (consider using kind or k3s)
- Cilium cluster mesh automation scripts

## Documentation
- Multicluster deployment guide (new doc in `docs/`)
- Cilium-specific examples in `config/samples/cilium/`
- Troubleshooting section in operator docs

## Testing Tools
- `cilium connectivity test` integration
- Network latency measurement tools
- Cross-cluster service discovery validation scripts
