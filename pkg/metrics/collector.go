// Package metrics implements the Prometheus collector that runs queries
// against OpenSearch in the background and exposes their results.
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

	// Static descriptors
	up            *prometheus.Desc
	queryDuration *prometheus.Desc
	querySuccess  *prometheus.Desc

	// Query results cache — keyed by query name, each value is a map of
	// metric-name to MetricGroup produced by the latest run of that query.
	queryMetrics      map[string]map[string]*parser.MetricGroup
	querySuccessState map[string]bool
	resultsMutex      sync.RWMutex

	// Background query execution
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewCollector creates a new metrics collector
func NewCollector(client *opensearch.Client, cfg *config.Config) *Collector {
	c := &Collector{
		client:            client,
		config:            cfg,
		queryMetrics:      make(map[string]map[string]*parser.MetricGroup),
		querySuccessState: make(map[string]bool),
		stopChan:          make(chan struct{}),

		up: prometheus.NewDesc(
			"opensearch_up",
			"Whether the OpenSearch cluster is reachable",
			nil, nil,
		),
		queryDuration: prometheus.NewDesc(
			"opensearch_query_duration_seconds",
			"Duration of the query in seconds",
			[]string{"query", "support_group", "service"}, nil,
		),
		querySuccess: prometheus.NewDesc(
			"opensearch_query_success",
			"Whether the query was successful",
			[]string{"query", "support_group", "service"}, nil,
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
}

// Collect implements prometheus.Collector
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.client.Ping(ctx); err != nil {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		slog.Warn("OpenSearch ping failed", "error", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	// Collect cluster health metrics (disabled by default)
	if c.config.CollectClusterHealth {
		c.collectClusterHealth(ctx, ch)
	}

	// Collect nodes stats (disabled by default)
	if c.config.CollectNodesStats {
		c.collectNodesStats(ctx, ch)
	}

	// Collect indices stats (disabled by default)
	if c.config.CollectIndicesStats {
		c.collectIndicesStats(ctx, ch)
	}

	// Collect query results
	c.collectQueryResults(ch)
}

func (c *Collector) collectClusterHealth(ctx context.Context, ch chan<- prometheus.Metric) {
	health, err := c.client.ClusterHealth(ctx)
	if err != nil {
		slog.Warn("Failed to get cluster health", "error", err)
		return
	}

	raw := parser.ParseClusterHealth(health, []string{"opensearch", "cluster_health"})
	grouped := parser.GroupMetrics(raw)
	for name, g := range grouped {
		for _, m := range g.ToPrometheus(name) {
			ch <- m
		}
	}
}

func (c *Collector) collectNodesStats(ctx context.Context, ch chan<- prometheus.Metric) {
	stats, err := c.client.NodesStats(ctx)
	if err != nil {
		slog.Warn("Failed to get nodes stats", "error", err)
		return
	}

	raw := parser.ParseNodesStats(stats, []string{"opensearch", "nodes_stats"})
	grouped := parser.GroupMetrics(raw)
	for name, g := range grouped {
		for _, m := range g.ToPrometheus(name) {
			ch <- m
		}
	}
}

func (c *Collector) collectIndicesStats(ctx context.Context, ch chan<- prometheus.Metric) {
	stats, err := c.client.IndicesStats(ctx)
	if err != nil {
		slog.Warn("Failed to get indices stats", "error", err)
		return
	}

	raw := parser.ParseIndicesStats(stats, false, []string{"opensearch", "indices_stats"})
	grouped := parser.GroupMetrics(raw)
	for name, g := range grouped {
		for _, m := range g.ToPrometheus(name) {
			ch <- m
		}
	}
}

func (c *Collector) collectQueryResults(ch chan<- prometheus.Metric) {
	c.resultsMutex.RLock()
	defer c.resultsMutex.RUnlock()

	for queryName, grouped := range c.queryMetrics {
		// Find query config for team
		query := findQuery(c.config.Queries, queryName)
		if query == nil {
			continue
		}

		// Emit success
		successVal := 0.0
		if c.querySuccessState[queryName] {
			successVal = 1.0
		}
		ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, successVal, queryName, query.SupportGroup, query.Service)

		// Emit grouped metrics
		for name, g := range grouped {
			for _, m := range g.ToPrometheus(name) {
				ch <- m
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

	slog.Debug("Executing query", "query", query.Name, "support_group", query.SupportGroup)

	// Execute the search
	response, err := c.client.Search(ctx, query.Indices, query.Query)
	duration := time.Since(start).Seconds()

	c.resultsMutex.Lock()
	defer c.resultsMutex.Unlock()

	oldGrouped := c.queryMetrics[query.Name]

	if err != nil {
		slog.Warn("Query failed", "query", query.Name, "error", err)
		c.querySuccessState[query.Name] = false
		if oldGrouped != nil {
			switch query.OnError {
			case config.StrategyPreserve:
				// keep old as-is
			case config.StrategyZero:
				c.queryMetrics[query.Name] = parser.MergeMetricGroups(oldGrouped, map[string]*parser.MetricGroup{}, true)
			case config.StrategyDrop:
				c.queryMetrics[query.Name] = map[string]*parser.MetricGroup{}
			}
		}
		return
	}

	// Parse the response into RawMetrics and group them
	raw := parser.ParseQueryResponse(response, query)
	newGrouped := parser.GroupMetrics(raw)

	// Handle missing metrics strategy
	if oldGrouped != nil && query.OnMissing != config.StrategyDrop {
		zeroMissing := query.OnMissing == config.StrategyZero
		newGrouped = parser.MergeMetricGroups(oldGrouped, newGrouped, zeroMissing)
	}

	// Add duration metric to the grouped results
	durationRM := parser.RawMetric{
		Name:   "opensearch_query_duration_seconds",
		Help:   "Duration of the query in seconds",
		Labels: []parser.Label{{Name: "query", Value: query.Name}, {Name: "support_group", Value: query.SupportGroup}, {Name: "service", Value: query.Service}},
		Value:  duration,
	}
	for name, g := range parser.GroupMetrics([]parser.RawMetric{durationRM}) {
		newGrouped[name] = g
	}

	c.queryMetrics[query.Name] = newGrouped
	c.querySuccessState[query.Name] = true
}

// findQuery looks up a query by name in the slice.
func findQuery(queries []config.Query, name string) *config.Query {
	for i := range queries {
		if queries[i].Name == name {
			return &queries[i]
		}
	}
	return nil
}

// Stop gracefully stops the collector
func (c *Collector) Stop() {
	close(c.stopChan)
	c.wg.Wait()
}
