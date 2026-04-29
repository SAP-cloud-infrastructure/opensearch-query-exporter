package parser

import (
	"testing"
)

// TestParseNodesStats_Basic verifies that a single node with JVM mem and OS CPU
// metrics produces RawMetrics with correct node_id and node_name labels.
func TestParseNodesStats_Basic(t *testing.T) {
	response := map[string]any{
		"_nodes": map[string]any{
			"total":      1.0,
			"successful": 1.0,
			"failed":     0.0,
		},
		"nodes": map[string]any{
			"node-abc-123": map[string]any{
				"name": "data-node-1",
				"jvm": map[string]any{
					"mem": map[string]any{
						"heap_used_in_bytes": 104857600.0,
					},
				},
				"os": map[string]any{
					"cpu": map[string]any{
						"percent": 42.0,
					},
				},
			},
		},
	}

	prefix := []string{"opensearch", "nodes_stats"}
	metrics := ParseNodesStats(response, prefix)
	if len(metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}

	// Every metric must carry node_id and node_name labels.
	for _, m := range metrics {
		if got := getLabelValue(m.Labels, "node_id"); got != "node-abc-123" {
			t.Errorf("metric %s: want node_id=node-abc-123, got %q", m.Name, got)
		}
		if got := getLabelValue(m.Labels, "node_name"); got != "data-node-1" {
			t.Errorf("metric %s: want node_name=data-node-1, got %q", m.Name, got)
		}
	}

	// Spot-check jvm.mem.heap_used_in_bytes metric.
	heapName := buildMetricName([]string{"opensearch", "nodes_stats", "jvm", "mem"}, "heap_used_in_bytes")
	heapMetric, ok := findMetricWithLabels(metrics, heapName, map[string]string{
		"node_id":   "node-abc-123",
		"node_name": "data-node-1",
	})
	if !ok {
		t.Fatalf("expected metric %q, not found; available: %v", heapName, allMetricNames(metrics))
	}
	if heapMetric.Value != 104857600.0 {
		t.Errorf("heap_used_in_bytes: want 104857600, got %v", heapMetric.Value)
	}

	// Spot-check os.cpu.percent metric.
	cpuName := buildMetricName([]string{"opensearch", "nodes_stats", "os", "cpu"}, "percent")
	cpuMetric, ok := findMetricWithLabels(metrics, cpuName, map[string]string{
		"node_id":   "node-abc-123",
		"node_name": "data-node-1",
	})
	if !ok {
		t.Fatalf("expected metric %q, not found; available: %v", cpuName, allMetricNames(metrics))
	}
	if cpuMetric.Value != 42.0 {
		t.Errorf("os cpu percent: want 42, got %v", cpuMetric.Value)
	}
}

// TestParseNodesStats_ThreadPool verifies that thread_pool dict entries produce
// metrics with a "thread_pool" label carrying the pool name.
func TestParseNodesStats_ThreadPool(t *testing.T) {
	response := map[string]any{
		"_nodes": map[string]any{
			"total":      1.0,
			"successful": 1.0,
			"failed":     0.0,
		},
		"nodes": map[string]any{
			"node-xyz": map[string]any{
				"name": "master-node",
				"thread_pool": map[string]any{
					"search": map[string]any{
						"threads":  8.0,
						"queue":    3.0,
						"rejected": 0.0,
					},
					"bulk": map[string]any{
						"threads":  4.0,
						"queue":    0.0,
						"rejected": 1.0,
					},
				},
			},
		},
	}

	prefix := []string{"opensearch", "nodes_stats"}
	metrics := ParseNodesStats(response, prefix)
	if len(metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}

	// Verify search.threads == 8 with thread_pool=search label.
	threadsName := buildMetricName([]string{"opensearch", "nodes_stats", "thread_pool"}, "threads")
	m, ok := findMetricWithLabels(metrics, threadsName, map[string]string{
		"node_id":     "node-xyz",
		"thread_pool": "search",
	})
	if !ok {
		t.Fatalf("expected metric %q with thread_pool=search, not found; available: %v", threadsName, allMetricNames(metrics))
	}
	if m.Value != 8.0 {
		t.Errorf("search threads: want 8, got %v", m.Value)
	}

	// Verify bulk.rejected == 1 with thread_pool=bulk label.
	rejectedName := buildMetricName([]string{"opensearch", "nodes_stats", "thread_pool"}, "rejected")
	m2, ok := findMetricWithLabels(metrics, rejectedName, map[string]string{
		"node_id":     "node-xyz",
		"thread_pool": "bulk",
	})
	if !ok {
		t.Fatalf("expected metric %q with thread_pool=bulk, not found; available: %v", rejectedName, allMetricNames(metrics))
	}
	if m2.Value != 1.0 {
		t.Errorf("bulk rejected: want 1, got %v", m2.Value)
	}
}

// TestParseNodesStats_FailedNodes verifies that a response with _nodes.failed > 0
// returns zero metrics.
func TestParseNodesStats_FailedNodes(t *testing.T) {
	response := map[string]any{
		"_nodes": map[string]any{
			"total":      2.0,
			"successful": 1.0,
			"failed":     1.0,
		},
		"nodes": map[string]any{
			"node-1": map[string]any{
				"name": "some-node",
				"os": map[string]any{
					"cpu": map[string]any{
						"percent": 10.0,
					},
				},
			},
		},
	}

	metrics := ParseNodesStats(response, nil)
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics when _nodes.failed > 0, got %d", len(metrics))
	}
}
