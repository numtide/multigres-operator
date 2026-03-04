# Operator Configuration

## Required Environment Variables

The operator requires two environment variables to be set on the Deployment (these are pre-configured in the install manifests):

| Variable | Description |
| :--- | :--- |
| `POD_NAMESPACE` | Namespace where the operator is deployed. Used for leader election, cert secrets, and cache filtering. |
| `POD_SERVICE_ACCOUNT` | Service account name of the operator pod. Used for webhook configuration. |

## Flags

You can customize the operator's behavior by passing flags to the binary (or editing the Deployment args).

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--webhook-enable` | `true` | Enable the admission webhook server. |
| `--webhook-port` | `9443` | The port that the webhook server serves at. |
| `--webhook-cert-dir` | `/var/run/secrets/webhook` | Directory to read/write webhook certificates. |
| `--webhook-service-name` | `multigres-operator-webhook-service` | Name of the Service pointing to the webhook. |
| `--webhook-service-namespace`| `$POD_NAMESPACE` | Namespace of the webhook service. |
| `--webhook-service-account` | `$POD_SERVICE_ACCOUNT` | Service Account name of the operator. |
| `--metrics-bind-address` | `:8443` | Address for the metrics endpoint. Set to `0` to disable. |
| `--metrics-secure` | `true` | Serve metrics over HTTPS with authentication and authorization. |
| `--enable-http2` | `false` | Enable HTTP/2 for metrics and webhook servers. |
| `--leader-elect` | `false` | Enable leader election (recommended for HA deployments). |

## Logging

The operator uses structured JSON logging (`zap` via controller-runtime). When tracing is enabled, every log line within a traced operation automatically includes `trace_id` and `span_id` fields.

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--zap-devel` | `true` | Development mode preset (console encoder, debug level, warn stacktraces). |
| `--zap-log-level` | depends on mode | Log verbosity: `debug`, `info`, `error`, or an integer. |
| `--zap-encoder` | depends on mode | Log format: `console` or `json`. |
| `--zap-stacktrace-level` | depends on mode | Minimum level that triggers stacktraces. |

| Setting | `--zap-devel=true` (default) | `--zap-devel=false` (production) |
| :--- | :--- | :--- |
| Default log level | `debug` | `info` |
| Encoder | `console` (human-readable) | `json` |
| Stacktraces from | `warn` | `error` |

> [!NOTE]
> For production deployments, set `--zap-devel=false` to switch to JSON encoding and `info`-level logging.
