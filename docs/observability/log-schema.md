# Log Schema

This document describes the expected Multigres runtime log shape after a
Kubernetes log collector enriches container stdout with pod metadata.

The operator is responsible for making the data available:
- structured logs to stdout
- consistent labels for pod discovery
- `multigres.com/project-ref` annotations for project attribution

The collector is responsible for enrichment and shipping.

## Required Fields

| Field | Type | Description |
|:---|:---|:---|
| `timestamp` | ISO-8601 string | Event timestamp emitted by the runtime |
| `level` | string | Log severity such as `debug`, `info`, `warn`, or `error` |
| `message` | string | Human-readable log message |
| `component` | string | Runtime component name |
| `project` | string | Supabase project ref from `multigres.com/project-ref`, or cluster-name fallback |
| `cluster` | string | Kubernetes cluster identity from `app.kubernetes.io/instance` |
| `namespace` | string | Kubernetes namespace containing the pod |
| `pod_name` | string | Kubernetes pod name |
| `node_name` | string | Kubernetes node name |

## Component Values

The `component` field should resolve to one of the following values for logs in
scope for this ticket:

| Runtime | `component` value |
|:---|:---|
| Postgres container in pool pods | `postgres` |
| Multipooler container in pool pods | `multipooler` |
| MultiOrch pod | `multiorch` |
| MultiGateway pod | `multigateway` |
| Postgres exporter sidecar in pool pods | `postgres-exporter` |

For `shard-pool` pods, the collector should use the container name to derive the
runtime component, since multiple containers share the same pod.

## Project Resolution

Collectors should derive the `project` field using this order:

1. `multigres.com/project-ref` pod annotation
2. `app.kubernetes.io/instance` pod label

In other words:
- explicit project ref wins when configured
- cluster name is the fallback when no explicit project ref is set

## Example Event

```json
{
  "timestamp": "2025-01-15T10:30:00.123Z",
  "level": "info",
  "message": "checkpoint complete: wrote 42 buffers",
  "component": "postgres",
  "project": "proj-abc123",
  "cluster": "my-cluster",
  "namespace": "multigres-prod",
  "pod_name": "my-cluster-postgres-default-0-pool-primary-zone1-0",
  "node_name": "ip-10-0-1-42.ec2.internal"
}
