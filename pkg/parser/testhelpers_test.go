package parser

import "testing"

// findMetricByName returns the first RawMetric whose Name equals name exactly.
func findMetricByName(metrics []RawMetric, name string) *RawMetric {
	for i := range metrics {
		if metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}

// findMetricsByName returns all RawMetrics whose Name equals name exactly.
func findMetricsByName(metrics []RawMetric, name string) []RawMetric {
	var result []RawMetric
	for _, m := range metrics {
		if m.Name == name {
			result = append(result, m)
		}
	}
	return result
}

// findMetricWithLabels returns the first RawMetric whose Name equals name and
// whose Labels include all of the wantLabels key-value pairs (extra labels are allowed).
func findMetricWithLabels(metrics []RawMetric, name string, wantLabels map[string]string) (RawMetric, bool) {
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

// getLabelValue returns the value of a label with the given name,
// or "" if absent.
func getLabelValue(labels []Label, labelName string) string {
	for _, l := range labels {
		if l.Name == labelName {
			return l.Value
		}
	}
	return ""
}

// allMetricNames returns all metric names in the slice for diagnostic output.
func allMetricNames(metrics []RawMetric) []string {
	names := make([]string, len(metrics))
	for i, m := range metrics {
		names[i] = m.Name
	}
	return names
}

// assertMetricValue asserts that a metric with the given name and labels exists
// and has the expected value.
func assertMetricValue(t *testing.T, metrics []RawMetric, name string, wantLabels map[string]string, wantValue float64) {
	t.Helper()
	m, ok := findMetricWithLabels(metrics, name, wantLabels)
	if !ok {
		t.Errorf("metric %q with labels %v not found; available metrics:", name, wantLabels)
		for _, rm := range metrics {
			t.Errorf("  name=%q labels=%v value=%v", rm.Name, rm.Labels, rm.Value)
		}
		return
	}
	if m.Value != wantValue {
		t.Errorf("metric %q: got value %v, want %v", name, m.Value, wantValue)
	}
}
