package parser

import (
	"testing"
)

// findMetric returns the first RawMetric whose Name equals name and whose
// Labels match all expected label pairs (extra labels are allowed).
// Returns (metric, true) if found, (zero, false) otherwise.
func findMetric(metrics []RawMetric, name string, wantLabels map[string]string) (RawMetric, bool) {
	for _, m := range metrics {
		if m.Name != name {
			continue
		}
		labelMap := make(map[string]string, len(m.Labels))
		for _, l := range m.Labels {
			labelMap[l.Name] = l.Value
		}
		match := true
		for k, v := range wantLabels {
			if labelMap[k] != v {
				match = false
				break
			}
		}
		if match {
			return m, true
		}
	}
	return RawMetric{}, false
}

// labelValue returns the value of a label with the given name, or "" if absent.
func labelValue(labels []Label, name string) string {
	for _, l := range labels {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

// TestParseNodesStats_Basic verifies that a single node with JVM mem and OS CPU
// metrics produces RawMetrics with correct node_id and node_name labels.
func TestParseNodesStats_Basic(t *testing.T) {
	response := map[string]interface{}{
		"_nodes": map[string]interface{}{
			"total":      1.0,
			"successful": 1.0,
			"failed":     0.0,
		},
		"nodes": map[string]interface{}{
			"node-abc-123": map[string]interface{}{
				"name": "data-node-1",
				"jvm": map[string]interface{}{
					"mem": map[string]interface{}{
						"heap_used_in_bytes": 104857600.0,
					},
				},
				"os": map[string]interface{}{
					"cpu": map[string]interface{}{
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
		if got := labelValue(m.Labels, "node_id"); got != "node-abc-123" {
			t.Errorf("metric %s: want node_id=node-abc-123, got %q", m.Name, got)
		}
		if got := labelValue(m.Labels, "node_name"); got != "data-node-1" {
			t.Errorf("metric %s: want node_name=data-node-1, got %q", m.Name, got)
		}
	}

	// Spot-check jvm.mem.heap_used_in_bytes metric.
	heapName := buildMetricName([]string{"opensearch", "nodes_stats", "jvm", "mem"}, "heap_used_in_bytes")
	heapMetric, ok := findMetric(metrics, heapName, map[string]string{
		"node_id":   "node-abc-123",
		"node_name": "data-node-1",
	})
	if !ok {
		t.Fatalf("expected metric %q, not found; available: %v", heapName, metricNames(metrics))
	}
	if heapMetric.Value != 104857600.0 {
		t.Errorf("heap_used_in_bytes: want 104857600, got %v", heapMetric.Value)
	}

	// Spot-check os.cpu.percent metric.
	cpuName := buildMetricName([]string{"opensearch", "nodes_stats", "os", "cpu"}, "percent")
	cpuMetric, ok := findMetric(metrics, cpuName, map[string]string{
		"node_id":   "node-abc-123",
		"node_name": "data-node-1",
	})
	if !ok {
		t.Fatalf("expected metric %q, not found; available: %v", cpuName, metricNames(metrics))
	}
	if cpuMetric.Value != 42.0 {
		t.Errorf("os cpu percent: want 42, got %v", cpuMetric.Value)
	}
}

// TestParseNodesStats_ThreadPool verifies that thread_pool dict entries produce
// metrics with a "thread_pool" label carrying the pool name.
func TestParseNodesStats_ThreadPool(t *testing.T) {
	response := map[string]interface{}{
		"_nodes": map[string]interface{}{
			"total":      1.0,
			"successful": 1.0,
			"failed":     0.0,
		},
		"nodes": map[string]interface{}{
			"node-xyz": map[string]interface{}{
				"name": "master-node",
				"thread_pool": map[string]interface{}{
					"search": map[string]interface{}{
						"threads":  8.0,
						"queue":    3.0,
						"rejected": 0.0,
					},
					"bulk": map[string]interface{}{
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
	m, ok := findMetric(metrics, threadsName, map[string]string{
		"node_id":     "node-xyz",
		"thread_pool": "search",
	})
	if !ok {
		t.Fatalf("expected metric %q with thread_pool=search, not found; available: %v", threadsName, metricNames(metrics))
	}
	if m.Value != 8.0 {
		t.Errorf("search threads: want 8, got %v", m.Value)
	}

	// Verify bulk.rejected == 1 with thread_pool=bulk label.
	rejectedName := buildMetricName([]string{"opensearch", "nodes_stats", "thread_pool"}, "rejected")
	m2, ok := findMetric(metrics, rejectedName, map[string]string{
		"node_id":     "node-xyz",
		"thread_pool": "bulk",
	})
	if !ok {
		t.Fatalf("expected metric %q with thread_pool=bulk, not found; available: %v", rejectedName, metricNames(metrics))
	}
	if m2.Value != 1.0 {
		t.Errorf("bulk rejected: want 1, got %v", m2.Value)
	}
}

// TestParseNodesStats_FailedNodes verifies that a response with _nodes.failed > 0
// returns zero metrics.
func TestParseNodesStats_FailedNodes(t *testing.T) {
	response := map[string]interface{}{
		"_nodes": map[string]interface{}{
			"total":      2.0,
			"successful": 1.0,
			"failed":     1.0,
		},
		"nodes": map[string]interface{}{
			"node-1": map[string]interface{}{
				"name": "some-node",
				"os": map[string]interface{}{
					"cpu": map[string]interface{}{
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

// metricNames returns all metric names in the slice for diagnostic output.
func metricNames(metrics []RawMetric) []string {
	names := make([]string, len(metrics))
	for i, m := range metrics {
		names[i] = m.Name
	}
	return names
}
