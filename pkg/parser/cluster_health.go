package parser

import (
	"sort"
	"strings"
)

// clusterHealthSingularForms maps plural dict keys to singular label names.
var clusterHealthSingularForms = map[string]string{
	"indices": "index",
	"shards":  "shard",
}

// ParseClusterHealth parses the JSON map returned by _cluster/health into
// a flat slice of RawMetric values.  metricPrefix is typically
// ["opensearch", "cluster_health"].
//
// Returns nil when the response has timed_out set to true.
func ParseClusterHealth(response map[string]interface{}, metricPrefix []string) []RawMetric {
	timedOut, _ := response["timed_out"].(bool)
	if timedOut {
		return nil
	}

	// Shallow copy so we can delete fields without mutating the caller's map.
	shallow := make(map[string]interface{}, len(response))
	for k, v := range response {
		shallow[k] = v
	}
	delete(shallow, "timed_out")

	return parseHealthBlock(shallow, metricPrefix, nil)
}

// parseHealthBlock recursively converts a cluster-health block into RawMetrics.
func parseHealthBlock(block map[string]interface{}, metricPrefix []string, labels []Label) []RawMetric {
	var metrics []RawMetric

	// Handle status specially: numeric value + per-colour binary metrics.
	if statusRaw, ok := block["status"]; ok {
		if statusStr, ok := statusRaw.(string); ok {
			statusValues := map[string]float64{
				"green":  0,
				"yellow": 1,
				"red":    2,
			}
			numericVal, known := statusValues[strings.ToLower(statusStr)]
			if !known {
				numericVal = -1
			}

			statusMetricName := buildMetricName(metricPrefix, "status")
			metrics = append(metrics, RawMetric{
				Name:   statusMetricName,
				Help:   "Cluster status (0=green, 1=yellow, 2=red)",
				Labels: cloneLabels(labels),
				Value:  numericVal,
			})

			for _, color := range []string{"green", "yellow", "red"} {
				var colorVal float64
				if strings.ToLower(statusStr) == color {
					colorVal = 1
				}
				metrics = append(metrics, RawMetric{
					Name:   buildMetricName(metricPrefix, "status_"+color),
					Help:   "1 if cluster status is " + color + ", 0 otherwise",
					Labels: cloneLabels(labels),
					Value:  colorVal,
				})
			}
		}
	}

	// Process remaining keys in sorted order for stable output.
	keys := make([]string, 0, len(block))
	for k := range block {
		if k == "status" {
			continue // already handled
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		val := block[key]

		switch v := val.(type) {
		case bool:
			var fv float64
			if v {
				fv = 1
			}
			metrics = append(metrics, RawMetric{
				Name:   buildMetricName(metricPrefix, key),
				Help:   "",
				Labels: cloneLabels(labels),
				Value:  fv,
			})

		case float64:
			metrics = append(metrics, RawMetric{
				Name:   buildMetricName(metricPrefix, key),
				Help:   "",
				Labels: cloneLabels(labels),
				Value:  v,
			})

		case map[string]interface{}:
			nestedPrefix := append(append([]string{}, metricPrefix...), key)

			if singularLabel, ok := clusterHealthSingularForms[key]; ok {
				// Each child key becomes a label value on the nested metrics.
				childKeys := make([]string, 0, len(v))
				for ck := range v {
					childKeys = append(childKeys, ck)
				}
				sort.Strings(childKeys)

				for _, childKey := range childKeys {
					childVal := v[childKey]
					if childBlock, ok := childVal.(map[string]interface{}); ok {
						childLabels := appendLabel(labels, singularLabel, childKey)
						sub := parseHealthBlock(childBlock, nestedPrefix, childLabels)
						metrics = append(metrics, sub...)
					}
				}
			} else {
				sub := parseHealthBlock(v, nestedPrefix, cloneLabels(labels))
				metrics = append(metrics, sub...)
			}
		}
	}

	return metrics
}
