// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TestParseClusterHealth_Basic
// ---------------------------------------------------------------------------

func TestParseClusterHealth_Basic(t *testing.T) {
	response := map[string]any{
		"cluster_name":                     "opensearch",
		"status":                           "green",
		"timed_out":                        false,
		"number_of_nodes":                  float64(3),
		"number_of_data_nodes":             float64(3),
		"discovered_master":                true,
		"discovered_cluster_manager":       true,
		"active_primary_shards":            float64(10),
		"active_shards":                    float64(20),
		"relocating_shards":                float64(0),
		"initializing_shards":              float64(0),
		"unassigned_shards":                float64(0),
		"delayed_unassigned_shards":        float64(0),
		"number_of_pending_tasks":          float64(0),
		"number_of_in_flight_fetch":        float64(0),
		"task_max_waiting_in_queue_millis": float64(0),
		"active_shards_percent_as_number":  float64(100),
	}

	prefix := []string{"opensearch", "cluster_health"}
	metrics := ParseClusterHealth(response, prefix)

	if len(metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}

	// Status numeric: green → 0
	assertMetricValue(t, metrics, "opensearch_cluster_health_status", nil, 0)

	// Status binary colours
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_green", nil, 1)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_yellow", nil, 0)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_red", nil, 0)

	// Numeric fields
	assertMetricValue(t, metrics, "opensearch_cluster_health_number_of_nodes", nil, 3)
	assertMetricValue(t, metrics, "opensearch_cluster_health_active_primary_shards", nil, 10)
	assertMetricValue(t, metrics, "opensearch_cluster_health_unassigned_shards", nil, 0)

	// Bool fields
	assertMetricValue(t, metrics, "opensearch_cluster_health_discovered_master", nil, 1)
	assertMetricValue(t, metrics, "opensearch_cluster_health_discovered_cluster_manager", nil, 1)

	// timed_out must NOT appear as a metric (it is consumed and deleted)
	for _, m := range metrics {
		if m.Name == "opensearch_cluster_health_timed_out" {
			t.Errorf("timed_out should not appear as a metric, but got %+v", m)
		}
	}
}

// ---------------------------------------------------------------------------
// TestParseClusterHealth_YellowStatus
// ---------------------------------------------------------------------------

func TestParseClusterHealth_YellowStatus(t *testing.T) {
	response := map[string]any{
		"status":            "yellow",
		"timed_out":         false,
		"number_of_nodes":   float64(1),
		"active_shards":     float64(5),
		"unassigned_shards": float64(2),
	}

	prefix := []string{"opensearch", "cluster_health"}
	metrics := ParseClusterHealth(response, prefix)

	assertMetricValue(t, metrics, "opensearch_cluster_health_status", nil, 1)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_green", nil, 0)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_yellow", nil, 1)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_red", nil, 0)
}

// ---------------------------------------------------------------------------
// TestParseClusterHealth_RedStatus
// ---------------------------------------------------------------------------

func TestParseClusterHealth_RedStatus(t *testing.T) {
	response := map[string]any{
		"status":    "red",
		"timed_out": false,
	}

	prefix := []string{"opensearch", "cluster_health"}
	metrics := ParseClusterHealth(response, prefix)

	assertMetricValue(t, metrics, "opensearch_cluster_health_status", nil, 2)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_green", nil, 0)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_yellow", nil, 0)
	assertMetricValue(t, metrics, "opensearch_cluster_health_status_red", nil, 1)
}

// ---------------------------------------------------------------------------
// TestParseClusterHealth_WithIndices
// ---------------------------------------------------------------------------

func TestParseClusterHealth_WithIndices(t *testing.T) {
	response := map[string]any{
		"status":    "yellow",
		"timed_out": false,
		"indices": map[string]any{
			"my-index": map[string]any{
				"status":                "yellow",
				"number_of_shards":      float64(3),
				"number_of_replicas":    float64(1),
				"active_primary_shards": float64(3),
				"active_shards":         float64(3),
				"relocating_shards":     float64(0),
				"initializing_shards":   float64(0),
				"unassigned_shards":     float64(3),
			},
			"other-index": map[string]any{
				"status":                "green",
				"number_of_shards":      float64(1),
				"number_of_replicas":    float64(1),
				"active_primary_shards": float64(1),
				"active_shards":         float64(2),
				"relocating_shards":     float64(0),
				"initializing_shards":   float64(0),
				"unassigned_shards":     float64(0),
			},
		},
	}

	prefix := []string{"opensearch", "cluster_health"}
	metrics := ParseClusterHealth(response, prefix)

	// Index-level metrics must carry an "index" label.
	assertMetricValue(t, metrics,
		"opensearch_cluster_health_indices_status",
		map[string]string{"index": "my-index"},
		1, // yellow
	)
	assertMetricValue(t, metrics,
		"opensearch_cluster_health_indices_status",
		map[string]string{"index": "other-index"},
		0, // green
	)
	assertMetricValue(t, metrics,
		"opensearch_cluster_health_indices_unassigned_shards",
		map[string]string{"index": "my-index"},
		3,
	)
	assertMetricValue(t, metrics,
		"opensearch_cluster_health_indices_unassigned_shards",
		map[string]string{"index": "other-index"},
		0,
	)
	assertMetricValue(t, metrics,
		"opensearch_cluster_health_indices_active_shards",
		map[string]string{"index": "my-index"},
		3,
	)
}

// ---------------------------------------------------------------------------
// TestParseClusterHealth_WithIndicesAndShards
// ---------------------------------------------------------------------------

func TestParseClusterHealth_WithIndicesAndShards(t *testing.T) {
	response := map[string]any{
		"status":    "green",
		"timed_out": false,
		"indices": map[string]any{
			"logs": map[string]any{
				"status": "green",
				"shards": map[string]any{
					"0": map[string]any{
						"status":              "green",
						"primary_active":      true,
						"active_shards":       float64(2),
						"relocating_shards":   float64(0),
						"initializing_shards": float64(0),
						"unassigned_shards":   float64(0),
					},
				},
			},
		},
	}

	prefix := []string{"opensearch", "cluster_health"}
	metrics := ParseClusterHealth(response, prefix)

	// Shard-level metrics must carry both "index" and "shard" labels.
	assertMetricValue(t, metrics,
		"opensearch_cluster_health_indices_shards_status",
		map[string]string{"index": "logs", "shard": "0"},
		0, // green
	)
	assertMetricValue(t, metrics,
		"opensearch_cluster_health_indices_shards_primary_active",
		map[string]string{"index": "logs", "shard": "0"},
		1, // bool true → 1
	)
}

// ---------------------------------------------------------------------------
// TestParseClusterHealth_TimedOut
// ---------------------------------------------------------------------------

func TestParseClusterHealth_TimedOut(t *testing.T) {
	response := map[string]any{
		"cluster_name":    "opensearch",
		"status":          "red",
		"timed_out":       true,
		"number_of_nodes": float64(3),
	}

	prefix := []string{"opensearch", "cluster_health"}
	metrics := ParseClusterHealth(response, prefix)

	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics when timed_out=true, got %d", len(metrics))
	}
}

// ---------------------------------------------------------------------------
// TestParseClusterHealth_TimedOutFalse_NotEmitted
// ---------------------------------------------------------------------------

func TestParseClusterHealth_TimedOutFalse_NotEmitted(t *testing.T) {
	response := map[string]any{
		"status":    "green",
		"timed_out": false,
	}

	prefix := []string{"opensearch", "cluster_health"}
	metrics := ParseClusterHealth(response, prefix)

	for _, m := range metrics {
		if m.Name == "opensearch_cluster_health_timed_out" {
			t.Errorf("timed_out=false should not be emitted as a metric")
		}
	}
}

// ---------------------------------------------------------------------------
// TestParseClusterHealth_StableOrder
// ---------------------------------------------------------------------------

func TestParseClusterHealth_StableOrder(t *testing.T) {
	response := map[string]any{
		"status":            "green",
		"timed_out":         false,
		"number_of_nodes":   float64(3),
		"active_shards":     float64(10),
		"unassigned_shards": float64(0),
	}

	prefix := []string{"opensearch", "cluster_health"}

	// Run twice and compare ordering.
	m1 := ParseClusterHealth(response, prefix)
	m2 := ParseClusterHealth(response, prefix)

	if len(m1) != len(m2) {
		t.Fatalf("inconsistent metric count: %d vs %d", len(m1), len(m2))
	}
	for i := range m1 {
		if m1[i].Name != m2[i].Name {
			t.Errorf("unstable order at index %d: %q vs %q", i, m1[i].Name, m2[i].Name)
		}
	}
}
