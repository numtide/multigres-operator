/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"os"

	corev1 "k8s.io/api/core/v1"
)

// BuildOTELEnvVars converts an ObservabilityConfig into a slice of
// environment variables suitable for injection into data-plane containers.
//
// Fallback behaviour: when cfg is nil or a field is empty, the operator's
// own environment variable (if set) is used as the default. This means
// data-plane pods automatically inherit the operator's telemetry endpoint
// unless explicitly overridden or disabled.
//
// Setting cfg.OTLPEndpoint to "disabled" suppresses all OTEL variables,
// returning nil regardless of other field values.
func BuildOTELEnvVars(cfg *ObservabilityConfig) []corev1.EnvVar {
	endpoint := envOrCRD(cfg, func(c *ObservabilityConfig) string { return c.OTLPEndpoint }, "OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" || endpoint == "disabled" {
		return nil
	}

	vars := []corev1.EnvVar{
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: endpoint},
	}

	appendIfSet := func(envName string, crdValue, fallbackEnv string) {
		v := crdValue
		if v == "" {
			v = os.Getenv(fallbackEnv)
		}
		if v != "" {
			vars = append(vars, corev1.EnvVar{Name: envName, Value: v})
		}
	}

	var proto, traces, metrics, logs, interval, temporality, sampler string
	if cfg != nil {
		proto = cfg.OTLPProtocol
		traces = cfg.TracesExporter
		metrics = cfg.MetricsExporter
		logs = cfg.LogsExporter
		interval = cfg.MetricExportInterval
		temporality = cfg.MetricsTemporality
		sampler = cfg.TracesSampler
	}

	appendIfSet("OTEL_EXPORTER_OTLP_PROTOCOL", proto, "OTEL_EXPORTER_OTLP_PROTOCOL")
	appendIfSet("OTEL_TRACES_EXPORTER", traces, "OTEL_TRACES_EXPORTER")
	appendIfSet("OTEL_METRICS_EXPORTER", metrics, "OTEL_METRICS_EXPORTER")
	appendIfSet("OTEL_LOGS_EXPORTER", logs, "OTEL_LOGS_EXPORTER")
	appendIfSet("OTEL_METRIC_EXPORT_INTERVAL", interval, "OTEL_METRIC_EXPORT_INTERVAL")
	appendIfSet("OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE", temporality, "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE")
	appendIfSet("OTEL_TRACES_SAMPLER", sampler, "OTEL_TRACES_SAMPLER")

	return vars
}

// envOrCRD returns the CRD field value if non-empty, otherwise falls back
// to the named environment variable.
func envOrCRD(cfg *ObservabilityConfig, getter func(*ObservabilityConfig) string, envName string) string {
	if cfg != nil {
		if v := getter(cfg); v != "" {
			return v
		}
	}
	return os.Getenv(envName)
}
