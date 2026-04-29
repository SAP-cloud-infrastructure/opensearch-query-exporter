package parser

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestRawMetric_ToPrometheus(t *testing.T) {
	rm := RawMetric{
		Name:  "my_metric",
		Help:  "a test metric",
		Value: 42.0,
		Labels: []Label{
			{Name: "env", Value: "prod"},
			{Name: "region", Value: "eu"},
		},
	}

	m := rm.ToPrometheus()
	if m == nil {
		t.Fatal("expected non-nil prometheus.Metric")
	}

	var metric dto.Metric
	if err := m.Write(&metric); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if metric.Gauge == nil || metric.Gauge.Value == nil {
		t.Fatal("expected gauge metric with value")
	}
	if *metric.Gauge.Value != 42.0 {
		t.Fatalf("expected value 42.0, got %f", *metric.Gauge.Value)
	}
	if len(metric.Label) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(metric.Label))
	}
}

func TestGroupMetrics(t *testing.T) {
	raw := []RawMetric{
		{Name: "http_requests", Help: "requests", Labels: []Label{{Name: "status", Value: "200"}}, Value: 10},
		{Name: "http_requests", Help: "requests", Labels: []Label{{Name: "status", Value: "500"}}, Value: 2},
		{Name: "http_errors", Help: "errors", Labels: []Label{{Name: "code", Value: "503"}}, Value: 1},
	}

	groups := GroupMetrics(raw)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	reqGroup, ok := groups["http_requests"]
	if !ok {
		t.Fatal("expected group 'http_requests'")
	}
	if len(reqGroup.Entries) != 2 {
		t.Fatalf("expected 2 entries in http_requests, got %d", len(reqGroup.Entries))
	}
	if len(reqGroup.LabelKeys) != 1 || reqGroup.LabelKeys[0] != "status" {
		t.Fatalf("expected label keys [status], got %v", reqGroup.LabelKeys)
	}

	errGroup, ok := groups["http_errors"]
	if !ok {
		t.Fatal("expected group 'http_errors'")
	}
	if len(errGroup.Entries) != 1 {
		t.Fatalf("expected 1 entry in http_errors, got %d", len(errGroup.Entries))
	}
}

func TestMergeMetricGroups_ZeroMissing(t *testing.T) {
	old := map[string]*MetricGroup{
		"req": {
			Help:      "requests",
			LabelKeys: []string{"a"},
			Entries: []MetricEntry{
				{LabelValues: []string{"1"}, Value: 10},
				{LabelValues: []string{"2"}, Value: 20},
			},
		},
	}
	newGroups := map[string]*MetricGroup{
		"req": {
			Help:      "requests",
			LabelKeys: []string{"a"},
			Entries: []MetricEntry{
				{LabelValues: []string{"1"}, Value: 30},
			},
		},
	}

	merged := MergeMetricGroups(old, newGroups, true)

	group, ok := merged["req"]
	if !ok {
		t.Fatal("expected group 'req' in merged result")
	}
	if len(group.Entries) != 2 {
		t.Fatalf("expected 2 entries after merge, got %d", len(group.Entries))
	}

	// Find entry with a=1 and a=2
	values := make(map[string]float64)
	for _, e := range group.Entries {
		values[e.LabelValues[0]] = e.Value
	}

	if values["1"] != 30 {
		t.Fatalf("expected a=1 to have value 30, got %f", values["1"])
	}
	if values["2"] != 0 {
		t.Fatalf("expected a=2 to be zeroed, got %f", values["2"])
	}
}

func TestMergeMetricGroups_PreserveMissing(t *testing.T) {
	old := map[string]*MetricGroup{
		"m1": {
			Help:      "metric1",
			LabelKeys: []string{"a"},
			Entries: []MetricEntry{
				{LabelValues: []string{"1"}, Value: 10},
			},
		},
	}
	newGroups := map[string]*MetricGroup{}

	merged := MergeMetricGroups(old, newGroups, false)

	group, ok := merged["m1"]
	if !ok {
		t.Fatal("expected group 'm1' to be preserved")
	}
	if len(group.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(group.Entries))
	}
	if group.Entries[0].Value != 10 {
		t.Fatalf("expected preserved value 10, got %f", group.Entries[0].Value)
	}
}

func TestMetricGroupToPrometheus(t *testing.T) {
	g := &MetricGroup{
		Help:      "test group",
		LabelKeys: []string{"status"},
		Entries: []MetricEntry{
			{LabelValues: []string{"200"}, Value: 100},
			{LabelValues: []string{"500"}, Value: 5},
		},
	}

	metrics := g.ToPrometheus("http_requests_total")
	if len(metrics) != 2 {
		t.Fatalf("expected 2 prometheus metrics, got %d", len(metrics))
	}

	for _, m := range metrics {
		if m == nil {
			t.Fatal("unexpected nil prometheus.Metric in result")
		}
		var metric dto.Metric
		if err := m.Write(&metric); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if metric.Gauge == nil || metric.Gauge.Value == nil {
			t.Fatal("expected gauge metric with value")
		}
	}
}
