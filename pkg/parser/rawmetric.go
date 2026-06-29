// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"slices"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// Label is an ordered name-value pair for a metric label.
type Label struct {
	Name  string
	Value string
}

// RawMetric is the intermediate representation for a parsed metric.
type RawMetric struct {
	Name   string
	Help   string
	Labels []Label
	Value  float64
}

// MetricEntry holds one set of label values and its metric value.
type MetricEntry struct {
	LabelValues []string
	Value       float64
}

// MetricGroup groups all entries for a metric name with the same label keys.
type MetricGroup struct {
	Help      string
	LabelKeys []string
	Entries   []MetricEntry
}

// ToPrometheus converts a single RawMetric to a Prometheus const gauge metric.
func (rm RawMetric) ToPrometheus() prometheus.Metric {
	labelNames := make([]string, len(rm.Labels))
	labelValues := make([]string, len(rm.Labels))
	for i, l := range rm.Labels {
		labelNames[i] = l.Name
		labelValues[i] = l.Value
	}

	desc := prometheus.NewDesc(rm.Name, rm.Help, labelNames, nil)
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, rm.Value, labelValues...)
}

// ToPrometheus converts a MetricGroup into Prometheus metrics using a shared descriptor.
func (g *MetricGroup) ToPrometheus(name string) []prometheus.Metric {
	desc := prometheus.NewDesc(name, g.Help, g.LabelKeys, nil)
	metrics := make([]prometheus.Metric, 0, len(g.Entries))
	for _, entry := range g.Entries {
		labelValues := slices.Clone(entry.LabelValues)
		metrics = append(metrics, prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, entry.Value, labelValues...))
	}
	return metrics
}

// GroupMetrics groups RawMetrics by name, extracting label keys from the first
// metric encountered with that name.
func GroupMetrics(raw []RawMetric) map[string]*MetricGroup {
	groups := make(map[string]*MetricGroup)
	for _, rm := range raw {
		group, exists := groups[rm.Name]
		if !exists {
			labelKeys := make([]string, len(rm.Labels))
			for i, l := range rm.Labels {
				labelKeys[i] = l.Name
			}
			group = &MetricGroup{
				Help:      rm.Help,
				LabelKeys: labelKeys,
			}
			groups[rm.Name] = group
		}

		labelValues := make([]string, len(rm.Labels))
		for i, l := range rm.Labels {
			labelValues[i] = l.Value
		}
		group.Entries = append(group.Entries, MetricEntry{
			LabelValues: labelValues,
			Value:       rm.Value,
		})
	}
	return groups
}

// entryKey returns a unique key for a MetricEntry based on its label values,
// joining them with the null byte separator \x00.
func entryKey(labelValues []string) string {
	return strings.Join(labelValues, "\x00")
}

// MergeMetricGroups merges old and new metric groups. New entries take
// precedence. For entries present in old but absent from new:
//   - if zeroMissing is true, the entry is included with value 0
//   - if zeroMissing is false, the old entry is preserved as-is
func MergeMetricGroups(old, new map[string]*MetricGroup, zeroMissing bool) map[string]*MetricGroup {
	result := make(map[string]*MetricGroup)

	// Copy all new groups into the result first.
	for name, newGroup := range new {
		result[name] = &MetricGroup{
			Help:      newGroup.Help,
			LabelKeys: slices.Clone(newGroup.LabelKeys),
			Entries:   slices.Clone(newGroup.Entries),
		}
	}

	// Process old groups to handle entries missing from new.
	for name, oldGroup := range old {
		newGroup, existsInNew := new[name]
		if !existsInNew {
			// Entire group is absent from new: preserve or zero all entries.
			merged := &MetricGroup{
				Help:      oldGroup.Help,
				LabelKeys: slices.Clone(oldGroup.LabelKeys),
			}
			for _, oldEntry := range oldGroup.Entries {
				e := MetricEntry{LabelValues: slices.Clone(oldEntry.LabelValues)}
				if zeroMissing {
					e.Value = 0
				} else {
					e.Value = oldEntry.Value
				}
				merged.Entries = append(merged.Entries, e)
			}
			result[name] = merged
			continue
		}

		// Group exists in both old and new. Build an index of new entries.
		newIndex := make(map[string]struct{}, len(newGroup.Entries))
		for _, e := range newGroup.Entries {
			newIndex[entryKey(e.LabelValues)] = struct{}{}
		}

		// Append old entries that are not present in new.
		resultGroup := result[name]
		for _, oldEntry := range oldGroup.Entries {
			if _, found := newIndex[entryKey(oldEntry.LabelValues)]; !found {
				e := MetricEntry{LabelValues: slices.Clone(oldEntry.LabelValues)}
				if zeroMissing {
					e.Value = 0
				} else {
					e.Value = oldEntry.Value
				}
				resultGroup.Entries = append(resultGroup.Entries, e)
			}
		}
	}

	return result
}
