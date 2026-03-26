package parser

import (
	"testing"
)

// TestParseIndicesStats_ClusterMode verifies that when parseIndices=false,
// _all data is parsed and every metric carries index="_all".
func TestParseIndicesStats_ClusterMode(t *testing.T) {
	response := map[string]interface{}{
		"_shards": map[string]interface{}{
			"total":      10.0,
			"successful": 10.0,
			"failed":     0.0,
		},
		"_all": map[string]interface{}{
			"primaries": map[string]interface{}{
				"docs": map[string]interface{}{
					"count":   1234.0,
					"deleted": 5.0,
				},
				"store": map[string]interface{}{
					"size_in_bytes": 5678.0,
				},
			},
		},
	}

	prefix := []string{"opensearch", "indices_stats"}
	metrics := ParseIndicesStats(response, false, prefix)
	if len(metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}

	// Every metric must carry index=_all.
	for _, m := range metrics {
		if got := labelValue(m.Labels, "index"); got != "_all" {
			t.Errorf("metric %s: want index=_all, got %q", m.Name, got)
		}
	}

	// Spot-check primaries.docs.count.
	docsCountName := buildMetricName([]string{"opensearch", "indices_stats", "primaries", "docs"}, "count")
	m, ok := findMetric(metrics, docsCountName, map[string]string{"index": "_all"})
	if !ok {
		t.Fatalf("expected metric %q, not found; available: %v", docsCountName, metricNames(metrics))
	}
	if m.Value != 1234.0 {
		t.Errorf("docs.count: want 1234, got %v", m.Value)
	}

	// Spot-check primaries.store.size_in_bytes.
	storeName := buildMetricName([]string{"opensearch", "indices_stats", "primaries", "store"}, "size_in_bytes")
	m2, ok := findMetric(metrics, storeName, map[string]string{"index": "_all"})
	if !ok {
		t.Fatalf("expected metric %q, not found; available: %v", storeName, metricNames(metrics))
	}
	if m2.Value != 5678.0 {
		t.Errorf("store.size_in_bytes: want 5678, got %v", m2.Value)
	}
}

// TestParseIndicesStats_IndicesMode verifies that when parseIndices=true,
// each index in response["indices"] is parsed and carries its own index label.
func TestParseIndicesStats_IndicesMode(t *testing.T) {
	response := map[string]interface{}{
		"_shards": map[string]interface{}{
			"total":      5.0,
			"successful": 5.0,
			"failed":     0.0,
		},
		"indices": map[string]interface{}{
			"my-index": map[string]interface{}{
				"primaries": map[string]interface{}{
					"docs": map[string]interface{}{
						"count":   42.0,
						"deleted": 0.0,
					},
				},
			},
		},
	}

	prefix := []string{"opensearch", "indices_stats"}
	metrics := ParseIndicesStats(response, true, prefix)
	if len(metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}

	// Every metric must carry index=my-index.
	for _, m := range metrics {
		if got := labelValue(m.Labels, "index"); got != "my-index" {
			t.Errorf("metric %s: want index=my-index, got %q", m.Name, got)
		}
	}

	// Spot-check primaries.docs.count for my-index.
	docsCountName := buildMetricName([]string{"opensearch", "indices_stats", "primaries", "docs"}, "count")
	m, ok := findMetric(metrics, docsCountName, map[string]string{"index": "my-index"})
	if !ok {
		t.Fatalf("expected metric %q with index=my-index, not found; available: %v", docsCountName, metricNames(metrics))
	}
	if m.Value != 42.0 {
		t.Errorf("docs.count: want 42, got %v", m.Value)
	}
}

// TestParseIndicesStats_FailedShards verifies that a response with
// _shards.failed > 0 returns zero metrics.
func TestParseIndicesStats_FailedShards(t *testing.T) {
	response := map[string]interface{}{
		"_shards": map[string]interface{}{
			"total":      10.0,
			"successful": 8.0,
			"failed":     2.0,
		},
		"_all": map[string]interface{}{
			"primaries": map[string]interface{}{
				"docs": map[string]interface{}{
					"count": 999.0,
				},
			},
		},
	}

	metricsCluster := ParseIndicesStats(response, false, []string{"opensearch", "indices_stats"})
	if len(metricsCluster) != 0 {
		t.Errorf("cluster mode: expected 0 metrics when _shards.failed > 0, got %d", len(metricsCluster))
	}

	metricsIndices := ParseIndicesStats(response, true, []string{"opensearch", "indices_stats"})
	if len(metricsIndices) != 0 {
		t.Errorf("indices mode: expected 0 metrics when _shards.failed > 0, got %d", len(metricsIndices))
	}
}

// TestParseIndicesStats_WithFields verifies that fielddata.fields dict entries
// produce metrics with a "field" label carrying the field name.
func TestParseIndicesStats_WithFields(t *testing.T) {
	response := map[string]interface{}{
		"_shards": map[string]interface{}{
			"total":      5.0,
			"successful": 5.0,
			"failed":     0.0,
		},
		"_all": map[string]interface{}{
			"primaries": map[string]interface{}{
				"fielddata": map[string]interface{}{
					"memory_size_in_bytes": 0.0,
					"evictions":            0.0,
					"fields": map[string]interface{}{
						"my_keyword_field": map[string]interface{}{
							"memory_size_in_bytes": 1024.0,
						},
						"another_field": map[string]interface{}{
							"memory_size_in_bytes": 512.0,
						},
					},
				},
			},
		},
	}

	prefix := []string{"opensearch", "indices_stats"}
	metrics := ParseIndicesStats(response, false, prefix)
	if len(metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}

	// Verify my_keyword_field entry has field label.
	fieldsMemName := buildMetricName(
		[]string{"opensearch", "indices_stats", "primaries", "fielddata", "fields"},
		"memory_size_in_bytes",
	)
	m, ok := findMetric(metrics, fieldsMemName, map[string]string{
		"index": "_all",
		"field": "my_keyword_field",
	})
	if !ok {
		t.Fatalf("expected metric %q with field=my_keyword_field, not found; available: %v", fieldsMemName, metricNames(metrics))
	}
	if m.Value != 1024.0 {
		t.Errorf("my_keyword_field memory_size_in_bytes: want 1024, got %v", m.Value)
	}

	// Verify another_field entry has field label and correct value.
	m2, ok := findMetric(metrics, fieldsMemName, map[string]string{
		"index": "_all",
		"field": "another_field",
	})
	if !ok {
		t.Fatalf("expected metric %q with field=another_field, not found; available: %v", fieldsMemName, metricNames(metrics))
	}
	if m2.Value != 512.0 {
		t.Errorf("another_field memory_size_in_bytes: want 512, got %v", m2.Value)
	}
}
