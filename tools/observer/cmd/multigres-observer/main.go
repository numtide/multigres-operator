package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/numtide/multigres-operator/tools/observer/pkg/common"
	"github.com/numtide/multigres-operator/tools/observer/pkg/observer"
	"github.com/numtide/multigres-operator/tools/observer/pkg/report"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
)

func main() {
	var (
		namespace         string
		operatorNamespace string
		interval          time.Duration
		kubeconfig        string
		once              bool
		metricsAddr       string
		logTailLines      int
		enableSQLProbe    bool
	)

	flag.StringVar(
		&namespace,
		"namespace",
		envString("NAMESPACE", ""),
		"Namespace to observe for multigres workloads (empty = all namespaces)",
	)
	flag.StringVar(
		&operatorNamespace,
		"operator-namespace",
		envString("OPERATOR_NAMESPACE", "multigres-operator"),
		"Namespace where the multigres operator is running",
	)
	flag.DurationVar(&interval, "interval", common.DefaultInterval, "Check interval")
	flag.StringVar(
		&kubeconfig,
		"kubeconfig",
		os.Getenv("KUBECONFIG"),
		"Path to kubeconfig (optional, uses in-cluster config by default)",
	)
	flag.BoolVar(&once, "once", false, "Run one observer cycle and exit (useful for CI)")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9090", "Address for Prometheus metrics endpoint")
	flag.IntVar(
		&logTailLines,
		"log-tail-lines",
		100,
		"Number of log lines to tail per component per cycle",
	)
	flag.BoolVar(
		&enableSQLProbe,
		"enable-sql-probe",
		true,
		"Enable SQL probes for replication health and connectivity checks",
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	logger.Info("multigres-observer starting",
		"version", version,
		"commit", gitCommit,
		"buildDate", buildDate,
		"namespace", namespace,
		"interval", interval,
	)

	clients, err := common.NewClients(kubeconfig)
	if err != nil {
		logger.Error("failed to create kubernetes clients", "error", err)
		os.Exit(1)
	}

	reg := prometheus.NewRegistry()
	metrics := report.NewMetrics(reg)
	reporter := report.NewReporter(logger, metrics)

	obs := observer.New(observer.Config{
		Client:            clients.Client,
		Clientset:         clients.Clientset,
		Reporter:          reporter,
		Metrics:           metrics,
		Namespace:         namespace,
		OperatorNamespace: operatorNamespace,
		Interval:          interval,
		Logger:            logger,
		LogTailLines:      logTailLines,
		EnableSQLProbe:    enableSQLProbe,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/status", obs.StatusHandler())

	srv := &http.Server{Addr: metricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("metrics server listening", "addr", metricsAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", "error", err)
		}
	}()

	if once {
		logger.Info("once mode: running single cycle")
		onceCtx, onceCancel := context.WithTimeout(ctx, 60*time.Second)
		defer onceCancel()
		_ = obs.Run(onceCtx)
		return
	}

	if err := obs.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("observer failed", "error", err)
		os.Exit(1)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
