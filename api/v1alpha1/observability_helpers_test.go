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
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestBuildOTELEnvVars(t *testing.T) {
	// Clear all OTEL env vars so host environment doesn't interfere.
	otelEnvVars := []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_TRACES_EXPORTER",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_METRIC_EXPORT_INTERVAL",
		"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE",
		"OTEL_TRACES_SAMPLER",
	}
	for _, env := range otelEnvVars {
		t.Setenv(env, "")
	}

	tests := map[string]struct {
		cfg  *ObservabilityConfig
		want []corev1.EnvVar
	}{
		"nil config returns nil": {
			cfg:  nil,
			want: nil,
		},
		"empty config returns nil": {
			cfg:  &ObservabilityConfig{},
			want: nil,
		},
		"disabled endpoint returns nil": {
			cfg:  &ObservabilityConfig{OTLPEndpoint: "disabled"},
			want: nil,
		},
		"endpoint only": {
			cfg: &ObservabilityConfig{
				OTLPEndpoint: "http://tempo:4318",
			},
			want: []corev1.EnvVar{
				{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://tempo:4318"},
			},
		},
		"all fields populated": {
			cfg: &ObservabilityConfig{
				OTLPEndpoint:         "http://tempo:4318",
				OTLPProtocol:         "grpc",
				TracesExporter:       "otlp",
				MetricsExporter:      "otlp",
				LogsExporter:         "otlp",
				MetricExportInterval: "30s",
				MetricsTemporality:   "cumulative",
				TracesSampler:        "always_on",
			},
			want: []corev1.EnvVar{
				{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://tempo:4318"},
				{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "grpc"},
				{Name: "OTEL_TRACES_EXPORTER", Value: "otlp"},
				{Name: "OTEL_METRICS_EXPORTER", Value: "otlp"},
				{Name: "OTEL_LOGS_EXPORTER", Value: "otlp"},
				{Name: "OTEL_METRIC_EXPORT_INTERVAL", Value: "30s"},
				{Name: "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE", Value: "cumulative"},
				{Name: "OTEL_TRACES_SAMPLER", Value: "always_on"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := BuildOTELEnvVars(tc.cfg)
			if len(got) != len(tc.want) {
				t.Fatalf(
					"len(BuildOTELEnvVars()) = %d, want %d\n  got:  %v\n  want: %v",
					len(got),
					len(tc.want),
					got,
					tc.want,
				)
			}
			for i := range got {
				if got[i].Name != tc.want[i].Name || got[i].Value != tc.want[i].Value {
					t.Errorf("env[%d] = {%q, %q}, want {%q, %q}",
						i, got[i].Name, got[i].Value, tc.want[i].Name, tc.want[i].Value)
				}
			}
		})
	}
}

func TestBuildOTELEnvVars_FallbackToEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://from-env:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	// nil config should fall back to env vars.
	got := BuildOTELEnvVars(nil)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 env vars from fallback, got %d: %v", len(got), got)
	}
	if got[0].Value != "http://from-env:4318" {
		t.Errorf("endpoint = %q, want %q", got[0].Value, "http://from-env:4318")
	}
	if got[1].Value != "http/protobuf" {
		t.Errorf("protocol = %q, want %q", got[1].Value, "http/protobuf")
	}
}

func TestBuildOTELEnvVars_CRDOverridesEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://from-env:4318")

	cfg := &ObservabilityConfig{
		OTLPEndpoint: "http://from-crd:4318",
	}
	got := BuildOTELEnvVars(cfg)
	if len(got) == 0 {
		t.Fatal("expected at least 1 env var")
	}
	if got[0].Value != "http://from-crd:4318" {
		t.Errorf("endpoint = %q, want CRD value %q", got[0].Value, "http://from-crd:4318")
	}
}

func TestEnvOrCRD(t *testing.T) {
	tests := map[string]struct {
		cfg    *ObservabilityConfig
		envVal string
		crdVal string
		want   string
	}{
		"nil config with env": {
			cfg:    nil,
			envVal: "from-env",
			want:   "from-env",
		},
		"nil config without env": {
			cfg:  nil,
			want: "",
		},
		"config with value": {
			cfg:    &ObservabilityConfig{OTLPEndpoint: "from-crd"},
			crdVal: "from-crd",
			want:   "from-crd",
		},
		"config with empty value falls back to env": {
			cfg:    &ObservabilityConfig{},
			envVal: "from-env",
			want:   "from-env",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_ENVORCRD", tc.envVal)
			got := envOrCRD(
				tc.cfg,
				func(c *ObservabilityConfig) string { return c.OTLPEndpoint },
				"TEST_ENVORCRD",
			)
			if got != tc.want {
				t.Errorf("envOrCRD() = %q, want %q", got, tc.want)
			}
		})
	}
}
