package parser

import (
	"sort"
	"strings"
	"testing"

	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/config"
)

func TestParseQueryResponse_TimedOut(t *testing.T) {
	resp := map[string]any{
		"timed_out": true,
		"hits": map[string]any{
			"total": map[string]any{"value": 100.0},
		},
		"took": 50.0,
	}
	q := config.Query{Name: "test", Team: "core"}
	metrics := ParseQueryResponse(resp, q)
	if len(metrics) != 0 {
		t.Fatalf("expected 0 metrics for timed_out response, got %d", len(metrics))
	}
}

func TestParseQueryResponse_HitsAndTook(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 123.0},
		},
		"took": 15.0,
	}
	q := config.Query{Name: "my query", Team: "core"}
	metrics := ParseQueryResponse(resp, q)

	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}

	hitsMetric := findMetricByName(metrics, "opensearch_query_my_query_hits")
	if hitsMetric == nil {
		t.Fatal("expected hits metric with name opensearch_query_my_query_hits")
	}
	if hitsMetric.Value != 123.0 {
		t.Fatalf("expected hits value 123, got %f", hitsMetric.Value)
	}

	tookMetric := findMetricByName(metrics, "opensearch_query_my_query_took_milliseconds")
	if tookMetric == nil {
		t.Fatal("expected took metric with name opensearch_query_my_query_took_milliseconds")
	}
	if tookMetric.Value != 15.0 {
		t.Fatalf("expected took value 15, got %f", tookMetric.Value)
	}
}

func TestParseQueryResponse_LegacyHitsTotal(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": 456.0, // Legacy format: total is a direct number
		},
	}
	q := config.Query{Name: "legacy", Team: "ops"}
	metrics := ParseQueryResponse(resp, q)

	hitsMetric := findMetricByName(metrics, "opensearch_query_legacy_hits")
	if hitsMetric == nil {
		t.Fatal("expected hits metric for legacy format")
	}
	if hitsMetric.Value != 456.0 {
		t.Fatalf("expected hits value 456, got %f", hitsMetric.Value)
	}
}

func TestParseQueryResponse_DictBuckets(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 10.0},
		},
		"aggregations": map[string]any{
			"status_filter": map[string]any{
				"buckets": map[string]any{
					"errors": map[string]any{
						"doc_count": 5.0,
					},
					"success": map[string]any{
						"doc_count": 95.0,
					},
				},
			},
		},
	}
	q := config.Query{Name: "q", Team: "sre"}
	metrics := ParseQueryResponse(resp, q)

	docCountMetrics := findMetricsByName(metrics, "opensearch_query_q_status_filter_doc_count")
	if len(docCountMetrics) != 2 {
		t.Fatalf("expected 2 dict bucket doc_count metrics, got %d", len(docCountMetrics))
	}

	// Collect label values for the status_filter label
	labelMap := make(map[string]float64)
	for _, m := range docCountMetrics {
		lv := getLabelValue(m.Labels, "status_filter")
		labelMap[lv] = m.Value
	}

	if labelMap["errors"] != 5.0 {
		t.Fatalf("expected errors doc_count 5, got %f", labelMap["errors"])
	}
	if labelMap["success"] != 95.0 {
		t.Fatalf("expected success doc_count 95, got %f", labelMap["success"])
	}
}

func TestParseQueryResponse_CompositeKeys(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 0.0},
		},
		"aggregations": map[string]any{
			"composite_agg": map[string]any{
				"buckets": []any{
					map[string]any{
						"key": map[string]any{
							"region":  "eu-west-1",
							"service": "api",
						},
						"doc_count": 42.0,
					},
					map[string]any{
						"key": map[string]any{
							"region":  "us-east-1",
							"service": "web",
						},
						"doc_count": 17.0,
					},
				},
			},
		},
	}
	q := config.Query{Name: "q", Team: "platform"}
	metrics := ParseQueryResponse(resp, q)

	docCountMetrics := findMetricsByName(metrics, "opensearch_query_q_composite_agg_doc_count")
	if len(docCountMetrics) != 2 {
		t.Fatalf("expected 2 composite bucket doc_count metrics, got %d", len(docCountMetrics))
	}

	// Check that composite key labels are present with the aggKey_compKey format
	for _, m := range docCountMetrics {
		regionVal := getLabelValue(m.Labels, "composite_agg_region")
		serviceVal := getLabelValue(m.Labels, "composite_agg_service")
		if regionVal == "" || serviceVal == "" {
			t.Fatalf("expected composite labels composite_agg_region and composite_agg_service, got labels: %v", m.Labels)
		}
	}

	// Check that labels are sorted (region before service)
	for _, m := range docCountMetrics {
		if len(m.Labels) < 2 {
			t.Fatalf("expected at least 2 labels, got %d", len(m.Labels))
		}
		labelNames := make([]string, len(m.Labels))
		for i, l := range m.Labels {
			labelNames[i] = l.Name
		}
		if !sort.StringsAreSorted(labelNames) {
			t.Fatalf("expected sorted label names, got %v", labelNames)
		}
	}
}

func TestParseQueryResponse_AllNumericFieldsInBucket(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 100.0},
		},
		"aggregations": map[string]any{
			"by_service": map[string]any{
				"buckets": []any{
					map[string]any{
						"key":       "svc-a",
						"doc_count": 50.0,
						"avg_latency": map[string]any{
							"value": 120.5,
						},
						"max_latency": map[string]any{
							"value": 500.0,
						},
					},
				},
			},
		},
	}
	q := config.Query{Name: "q", Team: "sre"}
	metrics := ParseQueryResponse(resp, q)

	// Should have: hits, doc_count, avg_latency.value, max_latency.value
	docCount := findMetricByName(metrics, "opensearch_query_q_by_service_doc_count")
	if docCount == nil {
		t.Fatal("expected doc_count metric")
	}
	if docCount.Value != 50.0 {
		t.Fatalf("expected doc_count 50, got %f", docCount.Value)
	}

	avgLatency := findMetricByName(metrics, "opensearch_query_q_by_service_avg_latency_value")
	if avgLatency == nil {
		t.Fatal("expected avg_latency value metric")
	}
	if avgLatency.Value != 120.5 {
		t.Fatalf("expected avg_latency 120.5, got %f", avgLatency.Value)
	}

	maxLatency := findMetricByName(metrics, "opensearch_query_q_by_service_max_latency_value")
	if maxLatency == nil {
		t.Fatal("expected max_latency value metric")
	}
	if maxLatency.Value != 500.0 {
		t.Fatalf("expected max_latency 500, got %f", maxLatency.Value)
	}

	// All bucket metrics should carry the by_service label
	for _, m := range []*RawMetric{docCount, avgLatency, maxLatency} {
		if getLabelValue(m.Labels, "by_service") != "svc-a" {
			t.Fatalf("expected by_service=svc-a label, got labels: %v", m.Labels)
		}
	}
}

func TestParseQueryResponse_CustomMetricMapping(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 10.0},
		},
		"custom": map[string]any{
			"value":  7.0,
			"labels": map[string]any{"env": "prod"},
		},
	}
	q := config.Query{
		Name: "errors_by_service",
		Team: "sre",
		Metrics: []config.MetricMapping{
			{
				Name:       "custom_metric",
				Path:       "custom.value",
				Help:       "A custom metric",
				Labels:     map[string]string{"static_label": "static_value"},
				LabelPaths: map[string]string{"env": "custom.labels.env"},
			},
		},
	}
	metrics := ParseQueryResponse(resp, q)

	customMetric := findMetricByName(metrics, "opensearch_query_errors_by_service_custom_metric")
	if customMetric == nil {
		t.Fatal("expected custom_metric")
	}
	if customMetric.Value != 7.0 {
		t.Fatalf("expected value 7, got %f", customMetric.Value)
	}
	if customMetric.Help != "A custom metric" {
		t.Fatalf("expected help 'A custom metric', got %q", customMetric.Help)
	}

	// Check static label
	if getLabelValue(customMetric.Labels, "static_label") != "static_value" {
		t.Fatalf("expected static_label=static_value, got labels: %v", customMetric.Labels)
	}

	// Check dynamic label from LabelPaths
	if getLabelValue(customMetric.Labels, "env") != "prod" {
		t.Fatalf("expected env=prod from LabelPaths, got labels: %v", customMetric.Labels)
	}
}

func TestParseQueryResponse_AfterKeySkipped(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 0.0},
		},
		"aggregations": map[string]any{
			"composite_agg": map[string]any{
				"after_key": map[string]any{
					"region": "eu-west-1",
				},
				"buckets": []any{
					map[string]any{
						"key":       map[string]any{"region": "eu-west-1"},
						"doc_count": 10.0,
					},
				},
			},
		},
	}
	q := config.Query{Name: "q", Team: "t"}
	metrics := ParseQueryResponse(resp, q)

	// after_key should be skipped — no metric with "after_key" in the name
	for _, m := range metrics {
		if strings.Contains(m.Name, "after_key") {
			t.Fatalf("after_key should be skipped, but found metric: %s", m.Name)
		}
	}
}

func TestParseQueryResponse_NestedSubAggregations(t *testing.T) {
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 100.0},
		},
		"aggregations": map[string]any{
			"by_region": map[string]any{
				"buckets": []any{
					map[string]any{
						"key":       "eu",
						"doc_count": 60.0,
						"by_status": map[string]any{
							"buckets": []any{
								map[string]any{
									"key":       "200",
									"doc_count": 50.0,
								},
								map[string]any{
									"key":       "500",
									"doc_count": 10.0,
								},
							},
						},
					},
				},
			},
		},
	}
	q := config.Query{Name: "q", Team: "t"}
	metrics := ParseQueryResponse(resp, q)

	// Find nested doc_count metrics for by_status
	nestedMetrics := findMetricsByName(metrics, "opensearch_query_q_by_region_by_status_doc_count")
	if len(nestedMetrics) != 2 {
		t.Fatalf("expected 2 nested by_status doc_count metrics, got %d", len(nestedMetrics))
	}

	// Each should have both by_region and by_status labels
	for _, m := range nestedMetrics {
		if getLabelValue(m.Labels, "by_region") != "eu" {
			t.Fatalf("expected by_region=eu, got %q", getLabelValue(m.Labels, "by_region"))
		}
		statusVal := getLabelValue(m.Labels, "by_status")
		if statusVal != "200" && statusVal != "500" {
			t.Fatalf("expected by_status to be 200 or 500, got %q", statusVal)
		}
	}
}

func TestParseQueryResponse_NoKeyBucket(t *testing.T) {
	// Buckets without keys (e.g., filter aggregation) should get filter_N labels
	resp := map[string]any{
		"hits": map[string]any{
			"total": map[string]any{"value": 10.0},
		},
		"aggregations": map[string]any{
			"filters_agg": map[string]any{
				"buckets": []any{
					map[string]any{
						"doc_count": 3.0,
					},
					map[string]any{
						"doc_count": 7.0,
					},
				},
			},
		},
	}
	q := config.Query{Name: "q", Team: "t"}
	metrics := ParseQueryResponse(resp, q)

	docCountMetrics := findMetricsByName(metrics, "opensearch_query_q_filters_agg_doc_count")
	if len(docCountMetrics) != 2 {
		t.Fatalf("expected 2 filter doc_count metrics, got %d", len(docCountMetrics))
	}

	// Check filter_N labels
	labelMap := map[string]float64{}
	for _, m := range docCountMetrics {
		lv := getLabelValue(m.Labels, "filters_agg")
		labelMap[lv] = m.Value
	}
	if labelMap["filter_0"] != 3.0 {
		t.Fatalf("expected filter_0 doc_count 3, got %f", labelMap["filter_0"])
	}
	if labelMap["filter_1"] != 7.0 {
		t.Fatalf("expected filter_1 doc_count 7, got %f", labelMap["filter_1"])
	}
}

func TestExtractValueFromPath_Success(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{
			"b": map[string]any{"c": 42.0},
		},
	}
	v, err := extractValueFromPath(data, "a.b.c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.(float64) != 42.0 {
		t.Fatalf("expected 42, got %v", v)
	}
}

func TestExtractValueFromPath_NotFound(t *testing.T) {
	data := map[string]any{"a": map[string]any{"b": 1.0}}
	if _, err := extractValueFromPath(data, "a.x.c"); err == nil {
		t.Fatalf("expected error for missing key")
	}
}

func TestToFloat64(t *testing.T) {
	cases := []struct {
		input    any
		expected float64
		ok       bool
	}{
		{float64(1.5), 1.5, true},
		{float32(2.5), 2.5, true},
		{int(3), 3.0, true},
		{int64(5), 5.0, true},
		{"not a number", 0, false},
		{nil, 0, false},
	}
	for _, tc := range cases {
		v, ok := toFloat64(tc.input)
		if ok != tc.ok {
			t.Fatalf("toFloat64(%v): expected ok=%v, got ok=%v", tc.input, tc.ok, ok)
		}
		if ok && v != tc.expected {
			t.Fatalf("toFloat64(%v): expected %f, got %f", tc.input, tc.expected, v)
		}
	}
}

func TestSanitizeHelpers(t *testing.T) {
	if got := sanitizeMetricName("Err ors-By@Service"); got != "err_ors_by_service" {
		t.Fatalf("sanitizeMetricName got %q", got)
	}
	if got := sanitizeLabelName("Err ors-By@Service"); got != "Err_ors_By_Service" {
		t.Fatalf("sanitizeLabelName got %q", got)
	}
}
