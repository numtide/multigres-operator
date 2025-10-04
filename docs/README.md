# Project Overview

## What is This?

Kubernetes operator for deploying and managing Multigres clusters.

## Core Principle

**Clean Separation**: The operator manages Kubernetes resources (Deployments, StatefulSets, Services) only. It has no knowledge of Multigres application internals. Multigres components handle their own startup dependencies and coordination.

## What Does It Deploy?

A Multigres cluster consists of:
- **etcd**: Distributed key-value store for cluster coordination
- **MultiGateway**: PostgreSQL protocol gateway
- **MultiOrch**: Orchestration service
- **MultiPooler**: Connection pooling with embedded PostgreSQL and pgctld

The operator creates and manages all necessary Kubernetes resources for these components.

## Technology

- **Language**: Go 1.24+
- **Framework**: Kubebuilder v3
- **Observability**: OpenTelemetry (traces, metrics, logs)
- **Testing**: Go testing + envtest (100% coverage goal)

## Project Structure

```
multigres-operator/
├── api/v1alpha1/      # CRD definitions
├── cmd/operator/      # Main entry point
├── internal/          # Controller and resource builders
├── config/            # Kubernetes manifests
├── docs/              # Architecture and guides
└── plans/             # Planning documents
```

## Documentation

- **architecture.md**: System design, components, technology choices
- **implementation-guide.md**: Development workflow, testing, coding standards
- **interface-design.md**: CRD API design, status fields, kubectl output

### Multigres Documentation

- https://multigres.com/: Documentation for Multigres architecture and design details
- https://github.com/multigres/multigres: Main repository for Multigres implementation
