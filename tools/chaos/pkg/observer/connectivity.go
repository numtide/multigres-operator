package observer

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/jackc/pgx/v5"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/common"
	"github.com/numtide/multigres-operator/tools/chaos/pkg/report"
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

		// HTTP health probes.
		o.probeHTTP(ctx, addr, common.PortMultiGatewayHTTP, "/live", "multigateway-liveness", svc.Name)
		o.probeHTTP(ctx, addr, common.PortMultiGatewayHTTP, "/ready", "multigateway-readiness", svc.Name)
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
		o.probeSQL(ctx, addr, common.PortMultiGatewayPG, svc.Name)
	}
}

func (o *Observer) probeSQL(ctx context.Context, host string, port int, component string) {
	connStr := fmt.Sprintf("host=%s port=%d user=postgres dbname=postgres connect_timeout=5 sslmode=disable", host, port)

	probeCtx, cancel := context.WithTimeout(ctx, common.ConnectivityTimeout)
	defer cancel()

	start := time.Now()
	conn, err := pgx.Connect(probeCtx, connStr)
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

	if latency > common.ConnectivityLatencyThreshold {
		o.reporter.Report(report.Finding{
			Severity:  report.SeverityWarn,
			Check:     "connectivity",
			Component: component,
			Message:   fmt.Sprintf("sql-probe: high latency %s for %s:%d", latency.Round(time.Millisecond), host, port),
		})
	}
}
