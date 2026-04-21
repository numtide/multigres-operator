# Multigres Operator Documentation

## User Documentation

| Document | Description |
|:---|:---|
| [README](../README.md) | Installation, configuration, resource hierarchy, and constraints |
| [Backup & Restore](backup-restore.md) | pgBackRest integration, S3 and filesystem backends, TLS certificates |
| [Observability](observability.md) | Metrics, alerts, dashboards, distributed tracing, structured logging |
| [Runtime Log Schema](observability/log-schema.md) | Field definitions for enriched Multigres runtime logs |
| [Durability Policy](durability-policy.md) | Durability policies, cross-AZ quorum, per-database overrides |
| [Storage Management](storage.md) | PVC deletion policies and volume expansion |
| [External Gateway](external-gateway.md) | External multigateway exposure, DNS wiring, GatewayExternalReady condition |
| [External Admin Web](external-admin-web.md) | External multiadmin-web exposure, AdminWebExternalReady condition |
| [PostgreSQL Initialization](postgresql-initialization.md) | Custom initdb arguments (locale, encoding, WAL segment size) |
| [PostgreSQL Configuration](postgresql-configuration.md) | Custom postgresql.conf parameters (memory, connections, WAL, tuning) |
| [Configuration Reference](configuration.md) | Operator flags, environment variables, and logging |
| [Operator Capability Levels](operator-capability-levels.md) | Assessment against Operator Framework capability model |
| [Sample Configurations](../config/samples/README.md) | YAML examples with override hierarchy walkthrough |

## Demos

Guided walkthroughs for specific user journeys: [demo/](../demo/)

| Demo | Description |
|:---|:---|
| [Webhook & Admission Control](../demo/webhook/demo.md) | Hybrid admission architecture, template resolution, safety gates |
| [Cert-Manager Integration](../demo/cert-manager/) | External certificate management with cert-manager |
| [Observability Stack](../demo/observability/) | Full metrics, tracing, and dashboards setup |

## Operations

| Document | Description |
|:---|:---|
| [Alert Runbooks](monitoring/runbooks/) | Investigation and remediation guides for all 10 operator alerts |

## Developer Documentation

Internal architecture and implementation references: [docs/development/](development/)

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for development setup, code style, and making changes.

## Changelog

See [CHANGELOG.md](../CHANGELOG.md) for release history.
