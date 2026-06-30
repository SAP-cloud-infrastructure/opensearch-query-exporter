// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/config"
	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/opensearch"
)

func newTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/_cluster/health", func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"cluster_name":          "test",
			"status":                "yellow",
			"timed_out":             false,
			"number_of_nodes":       float64(3),
			"active_primary_shards": float64(5),
			"active_shards":         float64(10),
		})
	})
	mux.HandleFunc("/_nodes/stats", func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"_nodes": map[string]any{"total": 1.0, "successful": 1.0, "failed": 0.0},
			"nodes":  map[string]any{},
		})
	})
	mux.HandleFunc("/_stats", func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"_shards": map[string]any{"total": 1.0, "successful": 1.0, "failed": 0.0},
			"_all":    map[string]any{},
		})
	})
	mux.HandleFunc("/idx/_search", func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"took":      float64(12),
			"timed_out": false,
			"hits": map[string]any{
				"total": map[string]any{"value": float64(42)},
			},
		})
	})
	srv := httptest.NewTLSServer(mux)
	// Force TLS1.2+ like the client
	srv.TLS = &tls.Config{Certificates: srv.TLS.Certificates, MinVersion: tls.VersionTLS12}
	return srv
}

func waitForQueryResult(t *testing.T, c *Collector, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.resultsMutex.RLock()
		_, ok := c.queryMetrics[name]
		c.resultsMutex.RUnlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("query result %s not populated in time", name)
}

func findMetricFamily(mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

func TestCollector_UpAndQueryMetrics(t *testing.T) {
	srv := newTLSServer(t)
	defer srv.Close()

	cfg := &config.Config{
		OpenSearchURL:        srv.URL,
		Credentials:          []config.Credential{{Username: "u", Password: "p"}},
		Insecure:             true,
		Timeout:              2 * time.Second,
		CollectClusterHealth: true,
		Queries: []config.Query{{
			Name:         "my_query",
			SupportGroup: "observability",
			Service:      "logs",
			Interval:     100 * time.Millisecond,
			Indices:      "idx",
			Query:        map[string]any{"size": 0},
		}},
	}
	client, err := opensearch.NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	collector := NewCollector(client, cfg)
	t.Cleanup(func() { collector.Stop() })

	// Wait for background query to populate
	waitForQueryResult(t, collector, "my_query", time.Second)

	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	// opensearch_up
	if mf := findMetricFamily(mfs, "opensearch_up"); mf == nil || len(mf.Metric) == 0 || mf.Metric[0].GetGauge().GetValue() != 1 {
		t.Fatalf("expected opensearch_up=1, got %#v", mf)
	}

	// query success
	if mf := findMetricFamily(mfs, "opensearch_query_success"); mf == nil {
		t.Fatalf("missing opensearch_query_success")
	}

	// hits metric for my_query (parser outputs _hits, not _hits_total)
	if mf := findMetricFamily(mfs, "opensearch_query_my_query_hits"); mf == nil {
		t.Fatalf("missing hits metric for my_query")
	}

	// cluster health status should be present via dynamic parser
	if mf := findMetricFamily(mfs, "opensearch_cluster_health_status"); mf == nil {
		t.Fatalf("missing opensearch_cluster_health_status")
	}
}

func TestCollector_PingFailureSetsUpZero(t *testing.T) {
	// Server that always 401s
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{
		OpenSearchURL: srv.URL,
		Credentials:   []config.Credential{{Username: "u", Password: "p"}},
		Insecure:      true,
		Timeout:       1 * time.Second,
		Queries:       nil,
	}
	client, err := opensearch.NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	collector := NewCollector(client, cfg)
	t.Cleanup(func() { collector.Stop() })

	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if mf := findMetricFamily(mfs, "opensearch_up"); mf == nil || mf.Metric[0].GetGauge().GetValue() != 0 {
		t.Fatalf("expected opensearch_up=0")
	}
}
