// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"fmt"
	"strings"
)

// indicesStatsBucketDictKeys are dict keys whose values are maps of named buckets.
// Each sub-key becomes a labeled dimension instead of a path segment.
var indicesStatsBucketDictKeys = map[string]bool{
	"fields": true,
}

// indicesStatsSingularForms maps a bucket-dict key to its singular label name.
var indicesStatsSingularForms = map[string]string{
	"fields": "field",
}

// ParseIndicesStats parses an _stats JSON response into []RawMetric.
//
// If parseIndices is true, each index in response["indices"] is parsed with an
// "index" label set to the index name.  If false, response["_all"] is parsed
// with index="_all".
//
// metricPrefix is the metric name prefix segments
// (e.g. []string{"opensearch", "indices_stats"}).
func ParseIndicesStats(response map[string]any, parseIndices bool, metricPrefix []string) []RawMetric {
	// Check _shards.failed — abort if any shards failed.
	if shardsBlock, ok := response["_shards"].(map[string]any); ok {
		if failed, ok := toFloat64(shardsBlock["failed"]); ok && failed > 0 {
			return nil
		}
	}

	var metrics []RawMetric

	if parseIndices {
		indices, ok := response["indices"].(map[string]any)
		if !ok {
			return nil
		}
		for _, indexName := range sortedKeys(indices) {
			indexData, ok := indices[indexName].(map[string]any)
			if !ok {
				continue
			}
			labels := []Label{{Name: "index", Value: indexName}}
			metrics = append(metrics, parseIndicesStatsBlock(indexData, metricPrefix, labels)...)
		}
	} else {
		allData, ok := response["_all"].(map[string]any)
		if !ok {
			return nil
		}
		labels := []Label{{Name: "index", Value: "_all"}}
		metrics = append(metrics, parseIndicesStatsBlock(allData, metricPrefix, labels)...)
	}

	return metrics
}

// parseIndicesStatsBlock recursively walks a stats block and emits RawMetrics.
// prefix is the accumulated metric name segments; labels are the accumulated
// Prometheus labels so far.
func parseIndicesStatsBlock(block map[string]any, prefix []string, labels []Label) []RawMetric {
	var metrics []RawMetric

	for _, key := range sortedKeys(block) {
		value := block[key]
		nextPrefix := append(append([]string{}, prefix...), key)

		switch v := value.(type) {
		case bool:
			var f float64
			if v {
				f = 1
			}
			metrics = append(metrics, RawMetric{
				Name:   buildMetricName(prefix, key),
				Help:   fmt.Sprintf("indices stats: %s", strings.Join(nextPrefix, ".")),
				Labels: cloneLabels(labels),
				Value:  f,
			})

		case map[string]any:
			if indicesStatsBucketDictKeys[key] {
				// Each sub-key is a bucket name; add a label for it.
				labelName := indicesStatsSingularForms[key]
				if labelName == "" {
					labelName = key
				}
				for _, bucketName := range sortedKeys(v) {
					bucketData, ok := v[bucketName].(map[string]any)
					if !ok {
						continue
					}
					bucketLabels := appendLabel(cloneLabels(labels), labelName, bucketName)
					metrics = append(metrics, parseIndicesStatsBlock(bucketData, nextPrefix, bucketLabels)...)
				}
			} else {
				metrics = append(metrics, parseIndicesStatsBlock(v, nextPrefix, labels)...)
			}

		case []any:
			// indices stats has no bucket list keys — lists are skipped.
			_ = v

		default:
			if f, ok := toFloat64(value); ok {
				metrics = append(metrics, RawMetric{
					Name:   buildMetricName(prefix, key),
					Help:   fmt.Sprintf("indices stats: %s", strings.Join(nextPrefix, ".")),
					Labels: cloneLabels(labels),
					Value:  f,
				})
			}
		}
	}

	return metrics
}
