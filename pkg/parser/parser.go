// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/SAP-cloud-infrastructure/opensearch-query-exporter/pkg/config"
)

// ParseQueryResponse parses an OpenSearch query response and returns RawMetrics.
// It handles hits, took, custom MetricMappings, and recursive aggregation parsing.
func ParseQueryResponse(response map[string]any, query config.Query) []RawMetric {
	// Check timed_out — return empty if true
	if timedOut, ok := response["timed_out"]; ok {
		if b, ok := timedOut.(bool); ok && b {
			return nil
		}
	}

	var metrics []RawMetric
	prefix := "opensearch_query_" + sanitizeMetricName(query.Name)

	// Parse hits
	if hits, ok := response["hits"].(map[string]any); ok {
		if total := extractHitsTotal(hits); total >= 0 {
			metrics = append(metrics, RawMetric{
				Name:  prefix + "_hits",
				Help:  "Total number of hits for the query",
				Value: total,
			})
		}
	}

	// Parse took time
	if took, ok := toFloat64(response["took"]); ok {
		metrics = append(metrics, RawMetric{
			Name:  prefix + "_took_milliseconds",
			Help:  "Time taken for the query in milliseconds",
			Value: took,
		})
	}

	// Parse configured MetricMappings
	for _, metricConfig := range query.Metrics {
		rm, err := extractMappedMetric(response, query, metricConfig)
		if err != nil {
			continue
		}
		metrics = append(metrics, rm)
	}

	// Parse aggregations if present
	if aggs, ok := response["aggregations"].(map[string]any); ok {
		aggMetrics := parseAgg("", aggs, []string{prefix}, nil)
		metrics = append(metrics, aggMetrics...)
	}

	return metrics
}

// extractHitsTotal extracts hits.total from either OpenSearch 2.x format (dict) or legacy format (number).
func extractHitsTotal(hits map[string]any) float64 {
	// OpenSearch 2.x format: {"total": {"value": N}}
	if total, ok := hits["total"].(map[string]any); ok {
		if value, ok := toFloat64(total["value"]); ok {
			return value
		}
	}
	// Legacy format: {"total": N}
	if total, ok := toFloat64(hits["total"]); ok {
		return total
	}
	return -1
}

// extractMappedMetric extracts a single RawMetric based on a MetricMapping configuration.
func extractMappedMetric(response map[string]any, query config.Query, metricConfig config.MetricMapping) (RawMetric, error) {
	value, err := extractValueFromPath(response, metricConfig.Path)
	if err != nil {
		return RawMetric{}, err
	}

	floatValue, ok := toFloat64(value)
	if !ok {
		return RawMetric{}, fmt.Errorf("path %s does not resolve to a numeric value", metricConfig.Path)
	}

	var labels []Label

	// Add static labels (sorted for stability)
	if len(metricConfig.Labels) > 0 {
		staticKeys := make([]string, 0, len(metricConfig.Labels))
		for k := range metricConfig.Labels {
			staticKeys = append(staticKeys, k)
		}
		sort.Strings(staticKeys)
		for _, k := range staticKeys {
			labels = appendLabel(labels, k, metricConfig.Labels[k])
		}
	}

	// Add dynamic labels from paths (sorted for stability)
	if len(metricConfig.LabelPaths) > 0 {
		dynKeys := make([]string, 0, len(metricConfig.LabelPaths))
		for k := range metricConfig.LabelPaths {
			dynKeys = append(dynKeys, k)
		}
		sort.Strings(dynKeys)
		for _, labelName := range dynKeys {
			labelValue, err := extractValueFromPath(response, metricConfig.LabelPaths[labelName])
			if err == nil {
				labels = appendLabel(labels, labelName, fmt.Sprintf("%v", labelValue))
			}
		}
	}

	metricName := fmt.Sprintf("opensearch_query_%s_%s", sanitizeMetricName(query.Name), sanitizeMetricName(metricConfig.Name))
	help := metricConfig.Help
	if help == "" {
		help = fmt.Sprintf("Metric %s from query %s", metricConfig.Name, query.Name)
	}

	return RawMetric{
		Name:   metricName,
		Help:   help,
		Labels: labels,
		Value:  floatValue,
	}, nil
}

// parseAgg recursively parses an aggregation node and emits RawMetrics.
//
// Algorithm:
//   - For each key/value in the aggregation dict:
//   - "buckets" + list  → parseBuckets
//   - "buckets" + dict  → parseBucketsFixed
//   - "after_key" when "buckets" also present → skip (composite paging)
//   - dict value → recurse into sub-aggregation
//   - numeric value → emit metric
func parseAgg(aggKey string, agg map[string]any, metricPrefix []string, labels []Label) []RawMetric {
	var metrics []RawMetric

	// Check if "buckets" exists in this aggregation (for after_key skipping)
	_, hasBuckets := agg["buckets"]

	// Sort keys for deterministic output
	keys := make([]string, 0, len(agg))
	for k := range agg {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := agg[key]

		if key == "buckets" {
			if isList(value) {
				bucketList := value.([]any)
				metrics = append(metrics, parseBuckets(aggKey, bucketList, metricPrefix, labels)...)
			} else if isDict(value) {
				bucketDict := value.(map[string]any)
				metrics = append(metrics, parseBucketsFixed(aggKey, bucketDict, metricPrefix, labels)...)
			}
			continue
		}

		// Skip after_key when buckets is present in same aggregation (composite paging)
		if key == "after_key" && hasBuckets {
			continue
		}

		if isDict(value) {
			// Recurse into sub-object aggregations
			subAgg := value.(map[string]any)
			subPrefix := append(clonePrefix(metricPrefix), sanitizeMetricName(key))
			metrics = append(metrics, parseAgg(key, subAgg, subPrefix, cloneLabels(labels))...)
			continue
		}

		if v, ok := toFloat64(value); ok {
			// Emit numeric field as a metric
			name := strings.Join(append(clonePrefix(metricPrefix), sanitizeMetricName(key)), "_")
			metrics = append(metrics, RawMetric{
				Name:   name,
				Help:   "Aggregation field " + key,
				Labels: cloneLabels(labels),
				Value:  v,
			})
		}
	}

	return metrics
}

// parseBuckets handles list-based buckets (the common case).
func parseBuckets(aggKey string, buckets []any, metricPrefix []string, labels []Label) []RawMetric {
	var metrics []RawMetric

	for i, item := range buckets {
		bucketMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		bucketLabels := cloneLabels(labels)

		if rawKey, hasKey := bucketMap["key"]; hasKey {
			if isDict(rawKey) {
				// Composite aggregation keys: key is a dict with multiple key/value pairs
				compositeKey := rawKey.(map[string]any)
				compKeys := make([]string, 0, len(compositeKey))
				for ck := range compositeKey {
					compKeys = append(compKeys, ck)
				}
				sort.Strings(compKeys)
				for _, ck := range compKeys {
					labelName := sanitizeLabelName(aggKey) + "_" + sanitizeLabelName(ck)
					bucketLabels = appendLabel(bucketLabels, labelName, fmt.Sprintf("%v", compositeKey[ck]))
				}
			} else {
				// Simple key
				bucketLabels = appendLabel(bucketLabels, sanitizeLabelName(aggKey), fmt.Sprintf("%v", rawKey))
			}
			delete(bucketMap, "key")
		} else {
			// No key — use filter_N pattern
			bucketLabels = appendLabel(bucketLabels, sanitizeLabelName(aggKey), fmt.Sprintf("filter_%d", i))
		}

		// Remove key_as_string — it's a display artifact
		delete(bucketMap, "key_as_string")

		// Recurse into the bucket itself
		metrics = append(metrics, parseAgg(aggKey, bucketMap, metricPrefix, bucketLabels)...)
	}

	return metrics
}

// parseBucketsFixed handles dict-based (fixed/named) buckets.
func parseBucketsFixed(aggKey string, buckets map[string]any, metricPrefix []string, labels []Label) []RawMetric {
	var metrics []RawMetric

	// Sort bucket keys for stability
	bucketKeys := make([]string, 0, len(buckets))
	for k := range buckets {
		bucketKeys = append(bucketKeys, k)
	}
	sort.Strings(bucketKeys)

	for _, bucketKey := range bucketKeys {
		bucketData := buckets[bucketKey]
		bucketMap, ok := bucketData.(map[string]any)
		if !ok {
			continue
		}

		bucketLabels := appendLabel(cloneLabels(labels), sanitizeLabelName(aggKey), bucketKey)
		metrics = append(metrics, parseAgg(aggKey, bucketMap, metricPrefix, bucketLabels)...)
	}

	return metrics
}

// extractValueFromPath navigates a nested map using a dot-separated path.
func extractValueFromPath(data any, path string) (any, error) {
	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = v[part]
			if !ok {
				return nil, fmt.Errorf("key %s not found in path %s", part, path)
			}
		default:
			return nil, fmt.Errorf("cannot navigate path %s: %s is not a map", path, part)
		}
	}

	return current, nil
}

// toFloat64 converts a JSON numeric value to float64.
// JSON only produces float64, so no reflect fallback is needed.
func toFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

// sanitizeMetricName replaces non-alphanumeric characters with underscores and lowercases.
func sanitizeMetricName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return strings.ToLower(result.String())
}

// sanitizeLabelName replaces non-alphanumeric characters with underscores, preserving case.
func sanitizeLabelName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}

// --- Helper functions ---

// sortedKeys returns the keys of m in sorted order for deterministic output.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// isList checks if v is a JSON array ([]any).
func isList(v any) bool {
	_, ok := v.([]any)
	return ok
}

// isDict checks if v is a JSON object (map[string]any).
func isDict(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// cloneLabels returns a copy of the labels slice.
func cloneLabels(labels []Label) []Label {
	if labels == nil {
		return nil
	}
	c := make([]Label, len(labels))
	copy(c, labels)
	return c
}

// appendLabel appends a new label and returns the extended slice.
func appendLabel(labels []Label, name, value string) []Label {
	return append(labels, Label{Name: name, Value: value})
}

// clonePrefix returns a copy of the string slice used as a metric name prefix.
func clonePrefix(prefix []string) []string {
	c := make([]string, len(prefix))
	copy(c, prefix)
	return c
}

// buildMetricName joins prefix parts and an optional suffix with underscores
// and sanitises the result.
func buildMetricName(prefix []string, suffix string) string {
	parts := clonePrefix(prefix)
	if suffix != "" {
		parts = append(parts, suffix)
	}
	return sanitizeMetricName(strings.Join(parts, "_"))
}
