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

	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/config"
	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/metrics"
	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/opensearch"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	listenAddress = flag.String("listen-address", ":9206", "Address to listen on for metrics")
	configPath    = flag.String("config", "config.yaml", "Path to configuration file")
	queriesDir    = flag.String("queries-dir", "", "Directory containing additional query files (*.yaml)")
	opensearchURL = flag.String("opensearch-url", "https://localhost:9200", "OpenSearch URL (must be https)")
	insecure      = flag.Bool("insecure", false, "Skip TLS certificate verification (insecure)")
	timeout       = flag.Duration("timeout", 30*time.Second, "Query timeout")
	logLevel      = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	// Set up structured logging
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Load additional queries from directory if specified
	if *queriesDir != "" {
		if err := config.LoadQueriesDir(cfg, *queriesDir); err != nil {
			slog.Error("Failed to load queries from directory", "dir", *queriesDir, "error", err)
			os.Exit(1)
		}
		slog.Info("Loaded queries from directory", "dir", *queriesDir, "total_queries", len(cfg.Queries))
	}

	// Override config with command line flags if provided
	if *opensearchURL != "https://localhost:9200" {
		cfg.OpenSearchURL = *opensearchURL
	}
	if *insecure {
		cfg.Insecure = true
	}
	if *timeout != 30*time.Second {
		cfg.Timeout = *timeout
	}

	// Create OpenSearch client
	client, err := opensearch.NewClient(cfg)
	if err != nil {
		slog.Error("Failed to create OpenSearch client", "error", err)
		os.Exit(1)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		slog.Error("Failed to connect to OpenSearch", "error", err)
		os.Exit(1)
	}

	slog.Info("Successfully connected to OpenSearch", "url", cfg.OpenSearchURL)

	// Create metrics collector
	collector := metrics.NewCollector(client, cfg)
	prometheus.MustRegister(collector)

	// Set up HTTP server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := client.Ping(ctx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
<head><title>OpenSearch Exporter</title></head>
<body>
<h1>OpenSearch Exporter</h1>
<p><a href="/metrics">Metrics</a></p>
</body>
</html>`))
	})

	server := &http.Server{
		Addr:         *listenAddress,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Starting server", "address", *listenAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	<-sigChan
	slog.Info("Shutting down...")

	collector.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	slog.Info("Server stopped")
}
