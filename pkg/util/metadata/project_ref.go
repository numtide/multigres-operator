package metadata

const (
	// AnnotationProjectRef carries the downstream-facing project identity used
	// by observability collectors. When absent, cluster name is the fallback.
	AnnotationProjectRef = "multigres.com/project-ref"

	// Prometheus scrape annotations for autodiscovery of exporter endpoints.
	AnnotationPrometheusScrape = "prometheus.io/scrape"
	AnnotationPrometheusPort   = "prometheus.io/port"
	AnnotationPrometheusPath   = "prometheus.io/path"
)

// ResolveProjectRef returns the explicit project ref when present and
// non-empty, otherwise it falls back to the cluster name.
func ResolveProjectRef(annotations map[string]string, clusterName string) string {
	if ref := annotations[AnnotationProjectRef]; ref != "" {
		return ref
	}
	return clusterName
}
