package observer

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/jackc/pgx/v5"
	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/tools/observer/pkg/common"
	"github.com/numtide/multigres-operator/tools/observer/pkg/report"
)

func (o *Observer) checkConnectivity(ctx context.Context) {
	o.probeMultiGatewayServices(ctx)
	o.probeMultiOrchServices(ctx)
	o.probeTopoServerServices(ctx)
	o.probePoolPodHealth(ctx)
	o.probeOperatorHealth(ctx)

	if o.enableSQLProbe {
		o.probeMultiGatewaySQLServices(ctx)
	}
}

func (o *Observer) probeMultiGatewayServices(ctx context.Context) {
	var svcs corev1.ServiceList
	if err := o.client.List(ctx, &svcs,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentMultiGateway,
		})...,
	); err != nil {
		return
	}

	for i := range svcs.Items {
		svc := &svcs.Items[i]
		addr := fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace)

		// TCP probe on PG port.
		o.probeTCP(addr, common.PortMultiGatewayPG, "multigateway-pg", svc.Name)

		// HTTP health probes — only on services that expose the HTTP port.
		// The global gateway service only exposes 15432, not 15100.
		if serviceHasPort(svc, common.PortMultiGatewayHTTP) {
			o.probeHTTP(ctx, addr, common.PortMultiGatewayHTTP, "/live", "multigateway-liveness", svc.Name)
			o.probeHTTP(ctx, addr, common.PortMultiGatewayHTTP, "/ready", "multigateway-readiness", svc.Name)
		}
	}
}

func (o *Observer) probeMultiOrchServices(ctx context.Context) {
	var svcs corev1.ServiceList
	if err := o.client.List(ctx, &svcs,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentMultiOrch,
		})...,
	); err != nil {
		return
	}

	for i := range svcs.Items {
		svc := &svcs.Items[i]
		addr := fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace)
		o.probeHTTP(ctx, addr, common.PortMultiOrchHTTP, "/live", "multiorch-liveness", svc.Name)
	}
}

func (o *Observer) probeTopoServerServices(ctx context.Context) {
	var svcs corev1.ServiceList
	if err := o.client.List(ctx, &svcs,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentGlobalTopo,
		})...,
	); err != nil {
		return
	}

	for i := range svcs.Items {
		svc := &svcs.Items[i]
		addr := fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace)
		o.probeHTTP(ctx, addr, common.PortEtcdClient, "/health", "etcd-health", svc.Name)
	}
}

func (o *Observer) probePoolPodHealth(ctx context.Context) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentPool,
		})...,
	); err != nil {
		return
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.PodIP == "" {
			continue
		}
		o.probeHTTP(ctx, pod.Status.PodIP, common.PortMultiPoolerHTTP, "/live", "multipooler-health", pod.Name)
	}
}

func (o *Observer) probeOperatorHealth(ctx context.Context) {
	var pods corev1.PodList
	if err := o.client.List(ctx, &pods,
		client.InNamespace(o.operatorNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"},
	); err != nil {
		return
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.PodIP == "" {
			continue
		}
		o.probeHTTP(ctx, pod.Status.PodIP, common.PortOperatorHealth, "/healthz", "operator-health", pod.Name)
		o.probeHTTP(ctx, pod.Status.PodIP, common.PortOperatorHealth, "/readyz", "operator-readiness", pod.Name)
	}
}

func (o *Observer) probeTCP(host string, port int, check, component string) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, common.ConnectivityTimeout)
	latency := time.Since(start)

	if o.metrics != nil {
		o.metrics.RecordProbeLatency(check, component, latency)
	}

	if o.probes != nil {
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		o.probes.RecordProbe(ProbeResult{
			Check: check, Component: component, Target: addr,
			OK: err == nil, Latency: latency.Round(time.Millisecond).String(), Error: errStr,
		})
	}

	if err != nil {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("%s: TCP probe failed for %s: %v", check, addr, err),
		})
		return
	}
	conn.Close()

	if latency > common.ConnectivityLatencyThreshold {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityWarn,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("%s: high latency %s connecting to %s", check, latency.Round(time.Millisecond), addr),
		})
	}
}

func (o *Observer) probeHTTP(ctx context.Context, host string, port int, path, check, component string) {
	url := fmt.Sprintf("http://%s:%d%s", host, port, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}

	start := time.Now()
	resp, err := o.httpClient.Do(req)
	latency := time.Since(start)

	if o.metrics != nil {
		o.metrics.RecordProbeLatency(check, component, latency)
	}

	ok := err == nil && resp != nil && resp.StatusCode == http.StatusOK
	if o.probes != nil {
		errStr := ""
		if err != nil {
			errStr = err.Error()
		} else if resp != nil && resp.StatusCode != http.StatusOK {
			errStr = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		o.probes.RecordProbe(ProbeResult{
			Check: check, Component: component, Target: url,
			OK: ok, Latency: latency.Round(time.Millisecond).String(), Error: errStr,
		})
	}

	if err != nil {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("%s: HTTP probe failed for %s: %v", check, url, err),
		})
		return
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		severity := report.SeverityError
		if resp.StatusCode == http.StatusServiceUnavailable {
			severity = report.SeverityWarn
		}
		o.reporter.Report(report.Finding{
			Severity:  severity,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("%s: %s returned HTTP %d", check, url, resp.StatusCode),
			Details:   map[string]any{"statusCode": resp.StatusCode},
		})
		return
	}

	if latency > common.ConnectivityLatencyThreshold {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityWarn,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("%s: high latency %s for %s", check, latency.Round(time.Millisecond), url),
		})
	}
}

func (o *Observer) probeMultiGatewaySQLServices(ctx context.Context) {
	var svcs corev1.ServiceList
	if err := o.client.List(ctx, &svcs,
		o.listOpts(client.MatchingLabels{
			common.LabelAppManagedBy: common.ManagedByMultigres,
			common.LabelAppComponent: common.ComponentMultiGateway,
		})...,
	); err != nil {
		return
	}

	for i := range svcs.Items {
		svc := &svcs.Items[i]
		addr := fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace)
		password := o.fetchGatewayPassword(ctx, svc)
		o.probeSQL(ctx, addr, common.PortMultiGatewayPG, svc.Name, password)
	}
}

// probeSQL connects to a PostgreSQL-compatible endpoint and runs SELECT 1.
//
// Uses simple query protocol because the multigateway does not yet support the
// extended query protocol's Describe step (fails with SQLSTATE MTD06). pgx defaults
// to extended protocol (Parse → Describe → Bind → Execute), which breaks through
// the gateway. Simple protocol sends the query as a single 'Q' message, which the
// gateway handles correctly.
func (o *Observer) probeSQL(ctx context.Context, host string, port int, component, password string) {
	if password == "" {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("sql-probe skipped for %s:%d: could not fetch postgres password from shard secret", host, port),
		})
		return
	}

	connStr := fmt.Sprintf("host=%s port=%d user=postgres dbname=postgres connect_timeout=5 sslmode=disable password=%s", host, port, password)

	probeCtx, cancel := context.WithTimeout(ctx, common.ConnectivityTimeout)
	defer cancel()

	connCfg, err := pgx.ParseConfig(connStr)
	if err != nil {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("sql-probe: invalid connection config for %s:%d: %v", host, port, err),
		})
		return
	}
	connCfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	start := time.Now()
	conn, err := pgx.ConnectConfig(probeCtx, connCfg)
	if err != nil {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("sql-probe: failed to connect to %s:%d: %v", host, port, err),
		})
		return
	}
	defer conn.Close(probeCtx)

	var result int
	if err := conn.QueryRow(probeCtx, "SELECT 1").Scan(&result); err != nil {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityError,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("sql-probe: SELECT 1 failed on %s:%d: %v", host, port, err),
		})
		return
	}

	latency := time.Since(start)
	if o.metrics != nil {
		o.metrics.RecordProbeLatency("sql-probe", component, latency)
	}

	if o.probes != nil {
		o.probes.RecordProbe(ProbeResult{
			Check: "sql-probe", Component: component,
			Target:  fmt.Sprintf("%s:%d", host, port),
			OK:      true,
			Latency: latency.Round(time.Millisecond).String(),
		})
	}

	if latency > common.ConnectivityLatencyThreshold {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityWarn,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("sql-probe: high latency %s for %s:%d", latency.Round(time.Millisecond), host, port),
		})
	}
}

// fetchGatewayPassword looks up a postgres password for the gateway's cluster.
// It finds a shard belonging to the same cluster and reads its password secret.
// Tries cluster label first, then falls back to instance label (per-zone services
// carry app.kubernetes.io/instance but not multigres.com/cluster).
func (o *Observer) fetchGatewayPassword(ctx context.Context, svc *corev1.Service) string {
	// Try cluster label first (global gateway services).
	if clusterName := svc.Labels[common.LabelMultigresCluster]; clusterName != "" {
		var shards multigresv1alpha1.ShardList
		if err := o.client.List(ctx, &shards,
			client.InNamespace(svc.Namespace),
			client.MatchingLabels{common.LabelMultigresCluster: clusterName},
		); err == nil && len(shards.Items) > 0 {
			return o.fetchShardPassword(ctx, &shards.Items[0])
		}
	}

	// Fall back to instance label (per-zone gateway services).
	if instance := svc.Labels[common.LabelAppInstance]; instance != "" {
		var shards multigresv1alpha1.ShardList
		if err := o.client.List(ctx, &shards,
			client.InNamespace(svc.Namespace),
			client.MatchingLabels{common.LabelMultigresCluster: instance},
		); err == nil && len(shards.Items) > 0 {
			return o.fetchShardPassword(ctx, &shards.Items[0])
		}
	}

	return ""
}

// fetchShardPassword reads the postgres superuser password from the per-shard secret.
func (o *Observer) fetchShardPassword(ctx context.Context, shard *multigresv1alpha1.Shard) string {
	secretName := shard.Name + "-postgres-password"
	var secret corev1.Secret
	if err := o.client.Get(ctx, types.NamespacedName{
		Namespace: shard.Namespace,
		Name:      secretName,
	}, &secret); err != nil {
		o.logger.Debug("failed to read postgres password secret", "secret", secretName, "error", err)
		return ""
	}

	pw, ok := secret.Data["password"]
	if !ok {
		return ""
	}
	return string(pw)
}

// serviceHasPort returns true if the service declares the given port.
func serviceHasPort(svc *corev1.Service, port int) bool {
	for _, p := range svc.Spec.Ports {
		if int(p.Port) == port {
			return true
		}
	}
	return false
}
