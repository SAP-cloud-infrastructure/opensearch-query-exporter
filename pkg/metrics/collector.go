package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/config"
	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/opensearch"
	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/parser"
)

// Collector implements the prometheus.Collector interface
type Collector struct {
	client *opensearch.Client
	config *config.Config

	// Metrics
	up                  *prometheus.Desc
	queryDuration       *prometheus.Desc
	querySuccess        *prometheus.Desc
	clusterHealthStatus *prometheus.Desc
	clusterHealthNodes  *prometheus.Desc
	clusterHealthShards *prometheus.Desc

	// Query results cache - stores current and previous results for error handling
	queryResults map[string]*queryResult
	resultsMutex sync.RWMutex

	// Background query execution
	stopChan chan struct{}
	wg       sync.WaitGroup
}

type queryResult struct {
	metrics         []prometheus.Metric
	previousMetrics []prometheus.Metric // For preserve/zero strategies
	timestamp       time.Time
	success         bool
}

// NewCollector creates a new metrics collector
func NewCollector(client *opensearch.Client, cfg *config.Config) *Collector {
	c := &Collector{
		client:       client,
		config:       cfg,
		queryResults: make(map[string]*queryResult),
		stopChan:     make(chan struct{}),

		up: prometheus.NewDesc(
			"opensearch_up",
			"Whether the OpenSearch cluster is reachable",
			nil, nil,
		),
		queryDuration: prometheus.NewDesc(
			"opensearch_query_duration_seconds",
			"Duration of the query in seconds",
			[]string{"query", "team"}, nil,
		),
		querySuccess: prometheus.NewDesc(
			"opensearch_query_success",
			"Whether the query was successful",
			[]string{"query", "team"}, nil,
		),
		clusterHealthStatus: prometheus.NewDesc(
			"opensearch_cluster_health_status",
			"Cluster health status (0=green, 1=yellow, 2=red)",
			[]string{"cluster"}, nil,
		),
		clusterHealthNodes: prometheus.NewDesc(
			"opensearch_cluster_health_nodes_total",
			"Total number of nodes in the cluster",
			[]string{"cluster"}, nil,
		),
		clusterHealthShards: prometheus.NewDesc(
			"opensearch_cluster_health_shards_total",
			"Total number of shards in the cluster",
			[]string{"cluster", "type"}, nil,
		),
	}

	// Start background query execution
	c.startQueryExecutors()

	return c
}

// Describe implements prometheus.Collector
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.queryDuration
	ch <- c.querySuccess
	ch <- c.clusterHealthStatus
	ch <- c.clusterHealthNodes
	ch <- c.clusterHealthShards
}

// Collect implements prometheus.Collector
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	// Check if OpenSearch is up
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.client.Ping(ctx); err != nil {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		slog.Warn("OpenSearch ping failed", "error", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	// Collect cluster health metrics
	c.collectClusterHealth(ctx, ch)

	// Collect query results
	c.collectQueryResults(ch)
}

func (c *Collector) collectClusterHealth(ctx context.Context, ch chan<- prometheus.Metric) {
	health, err := c.client.ClusterHealth(ctx)
	if err != nil {
		slog.Warn("Failed to get cluster health", "error", err)
		return
	}

	// Extract cluster name
	clusterName, _ := health["cluster_name"].(string)
	if clusterName == "" {
		clusterName = "unknown"
	}

	// Health status
	status, _ := health["status"].(string)
	statusValue := float64(2) // default to red
	switch status {
	case "green":
		statusValue = 0
	case "yellow":
		statusValue = 1
	}
	ch <- prometheus.MustNewConstMetric(c.clusterHealthStatus, prometheus.GaugeValue, statusValue, clusterName)

	// Number of nodes
	if nodes, ok := health["number_of_nodes"].(float64); ok {
		ch <- prometheus.MustNewConstMetric(c.clusterHealthNodes, prometheus.GaugeValue, nodes, clusterName)
	}

	// Shards
	if shards, ok := health["active_primary_shards"].(float64); ok {
		ch <- prometheus.MustNewConstMetric(c.clusterHealthShards, prometheus.GaugeValue, shards, clusterName, "primary")
	}
	if shards, ok := health["active_shards"].(float64); ok {
		ch <- prometheus.MustNewConstMetric(c.clusterHealthShards, prometheus.GaugeValue, shards, clusterName, "active")
	}
}

func (c *Collector) collectQueryResults(ch chan<- prometheus.Metric) {
	c.resultsMutex.RLock()
	defer c.resultsMutex.RUnlock()

	for queryName, result := range c.queryResults {
		// Find the query config
		var query *config.Query
		for i := range c.config.Queries {
			if c.config.Queries[i].Name == queryName {
				query = &c.config.Queries[i]
				break
			}
		}
		if query == nil {
			continue
		}

		if result.success {
			// Query succeeded - emit current metrics
			for _, metric := range result.metrics {
				ch <- metric
			}
		} else {
			// Query failed - apply OnError strategy
			ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 0, queryName, query.Team)

			switch query.OnError {
			case config.StrategyPreserve:
				// Keep metrics from last successful run
				for _, metric := range result.previousMetrics {
					ch <- metric
				}
			case config.StrategyZero:
				// Emit zeroed versions of previous metrics
				for _, metric := range zeroMetrics(result.previousMetrics) {
					ch <- metric
				}
			case config.StrategyDrop:
				// Don't emit any metrics (already handled by not iterating)
			}
		}
	}
}

func (c *Collector) startQueryExecutors() {
	for _, query := range c.config.Queries {
		c.wg.Add(1)
		go c.executeQueryPeriodically(query)
	}
}

func (c *Collector) executeQueryPeriodically(query config.Query) {
	defer c.wg.Done()

	// Execute immediately
	c.executeQuery(query)

	ticker := time.NewTicker(query.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.executeQuery(query)
		case <-c.stopChan:
			return
		}
	}
}

func (c *Collector) executeQuery(query config.Query) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	slog.Debug("Executing query", "query", query.Name, "team", query.Team)

	// Execute the search
	response, err := c.client.Search(ctx, query.Indices, query.Query)
	duration := time.Since(start).Seconds()

	c.resultsMutex.Lock()
	defer c.resultsMutex.Unlock()

	// Get previous result for error handling strategies
	previousResult := c.queryResults[query.Name]
	var previousMetrics []prometheus.Metric
	if previousResult != nil && previousResult.success {
		previousMetrics = previousResult.metrics
	} else if previousResult != nil {
		previousMetrics = previousResult.previousMetrics
	}

	result := &queryResult{
		timestamp:       time.Now(),
		previousMetrics: previousMetrics,
	}

	if err != nil {
		slog.Warn("Query failed", "query", query.Name, "error", err)
		result.success = false
		c.queryResults[query.Name] = result
		return
	}

	// Parse the response and extract metrics
	metrics, err := parser.ParseResponse(response, query)
	if err != nil {
		slog.Warn("Failed to parse response", "query", query.Name, "error", err)
		result.success = false
		c.queryResults[query.Name] = result
		return
	}

	// Handle OnMissing strategy - compare current metrics with previous
	if previousMetrics != nil && query.OnMissing != config.StrategyDrop {
		metrics = handleMissingMetrics(previousMetrics, metrics, query.OnMissing)
	}

	// Add query metadata metrics
	result.metrics = append(metrics,
		prometheus.MustNewConstMetric(c.queryDuration, prometheus.GaugeValue, duration, query.Name, query.Team),
		prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 1, query.Name, query.Team),
	)
	result.success = true

	c.queryResults[query.Name] = result
}

// zeroMetrics creates zeroed versions of the given metrics
func zeroMetrics(metrics []prometheus.Metric) []prometheus.Metric {
	// Note: This is a simplified implementation. In practice, we'd need to
	// extract the metric descriptors and labels and create new metrics with value 0.
	// For now, we just return an empty slice since prometheus.Metric doesn't
	// expose its value for modification.
	// A production implementation would need to track metric metadata separately.
	return nil
}

// handleMissingMetrics handles metrics that were present in previous run but not current
func handleMissingMetrics(previous, current []prometheus.Metric, strategy config.ErrorStrategy) []prometheus.Metric {
	// Note: This is a simplified implementation. A full implementation would need to:
	// 1. Track metric identities (name + labels)
	// 2. Compare previous vs current to find missing
	// 3. Apply preserve/zero strategy to missing ones
	//
	// For now, we just return current metrics as-is.
	// The OnMissing strategy is more relevant when specific label combinations disappear
	// (e.g., a service stops reporting), which requires metric identity tracking.
	return current
}

// Stop gracefully stops the collector
func (c *Collector) Stop() {
	close(c.stopChan)
	c.wg.Wait()
}
