package parser

import (
	"fmt"
	"strings"
)

// nodesStatsBucketDictKeys are dict keys whose values are maps of named buckets.
// Each bucket becomes a labeled dimension instead of a path segment.
var nodesStatsBucketDictKeys = map[string]bool{
	"pools":        true,
	"collectors":   true,
	"buffer_pools": true,
	"thread_pool":  true,
}

// nodesStatsSingularForms maps a bucket-dict key to its singular label name.
// Keys not listed here use the key itself as the label name.
var nodesStatsSingularForms = map[string]string{
	"pools":        "pool",
	"collectors":   "collector",
	"buffer_pools": "buffer_pool",
}

// nodesStatsBucketListKeys maps list keys to the field name inside each
// element that provides the label value.
var nodesStatsBucketListKeys = map[string]string{
	"data":    "path",
	"devices": "device_name",
}

// nodesStatsExcludedKeys are keys skipped during recursive traversal.
var nodesStatsExcludedKeys = map[string]bool{
	"timestamp": true,
}

// ParseNodesStats parses an _nodes/stats JSON response into []RawMetric.
// metricPrefix is the metric name prefix segments
// (e.g. []string{"opensearch", "nodes_stats"}).
func ParseNodesStats(response map[string]any, metricPrefix []string) []RawMetric {
	// Check _nodes.failed — abort if any node failed.
	if nodesBlock, ok := response["_nodes"].(map[string]any); ok {
		if failed, ok := toFloat64(nodesBlock["failed"]); ok && failed > 0 {
			return nil
		}
	}

	nodes, ok := response["nodes"].(map[string]any)
	if !ok {
		return nil
	}

	var metrics []RawMetric
	for _, nodeID := range sortedKeys(nodes) {
		nodeData, ok := nodes[nodeID].(map[string]any)
		if !ok {
			continue
		}
		nodeName, _ := nodeData["name"].(string)

		labels := []Label{
			{Name: "node_id", Value: nodeID},
			{Name: "node_name", Value: nodeName},
		}

		metrics = append(metrics, parseNodesStatsBlock(nodeData, metricPrefix, labels)...)
	}
	return metrics
}

// parseNodesStatsBlock recursively walks a stats block and emits RawMetrics.
// prefix is the accumulated metric name segments up to (but not including) the
// current block's parent key; each key is passed as suffix to buildMetricName.
// labels are the accumulated Prometheus labels so far.
func parseNodesStatsBlock(block map[string]any, prefix []string, labels []Label) []RawMetric {
	var metrics []RawMetric

	for _, key := range sortedKeys(block) {
		if nodesStatsExcludedKeys[key] {
			continue
		}

		value := block[key]
		// nextPrefix is the prefix to pass when recursing into sub-maps.
		nextPrefix := append(append([]string{}, prefix...), key)

		switch v := value.(type) {
		case bool:
			var f float64
			if v {
				f = 1
			}
			metrics = append(metrics, RawMetric{
				Name:   buildMetricName(prefix, key),
				Help:   fmt.Sprintf("nodes stats: %s", strings.Join(nextPrefix, ".")),
				Labels: cloneLabels(labels),
				Value:  f,
			})

		case map[string]any:
			if nodesStatsBucketDictKeys[key] {
				// Each sub-key is a bucket name; add a label for it.
				labelName := nodesStatsSingularForms[key]
				if labelName == "" {
					labelName = key
				}
				for _, bucketName := range sortedKeys(v) {
					bucketData, ok := v[bucketName].(map[string]any)
					if !ok {
						continue
					}
					bucketLabels := appendLabel(labels, labelName, bucketName)
					metrics = append(metrics, parseNodesStatsBlock(bucketData, nextPrefix, bucketLabels)...)
				}
			} else {
				metrics = append(metrics, parseNodesStatsBlock(v, nextPrefix, labels)...)
			}

		case []any:
			labelField, isBucketList := nodesStatsBucketListKeys[key]
			if isBucketList {
				for _, item := range v {
					itemMap, ok := item.(map[string]any)
					if !ok {
						continue
					}
					labelValue := fmt.Sprintf("%v", itemMap[labelField])
					itemLabels := appendLabel(labels, labelField, labelValue)
					metrics = append(metrics, parseNodesStatsBlock(itemMap, nextPrefix, itemLabels)...)
				}
			}
			// Lists that are not bucket lists are skipped (no numeric meaning).

		default:
			if f, ok := toFloat64(value); ok {
				metrics = append(metrics, RawMetric{
					Name:   buildMetricName(prefix, key),
					Help:   fmt.Sprintf("nodes stats: %s", strings.Join(nextPrefix, ".")),
					Labels: cloneLabels(labels),
					Value:  f,
				})
			}
		}
	}

	return metrics
}
