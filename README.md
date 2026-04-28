# OpenSearch Query Exporter

A Prometheus exporter that runs custom OpenSearch queries on a schedule and exposes results as metrics. Designed for Kubernetes deployments where teams define their own queries via ConfigMaps.

## Features

- **Query-focused**: Only exposes metrics you define, no noise from cluster internals
- **Secure**: TLS-only with credential failover support
- **Efficient**: Concurrent background query execution, minimal footprint
- **Resilient**: Configurable error handling (preserve/drop/zero) per query
- **Safe**: `max_query_range` rejects queries that would hit warm/cold storage
- **Kubernetes-native**: Health probes, env var expansion in config, ConfigMap-friendly

## Building

```bash
make build
make test
```

## Configuration

Environment variables are expanded in config files (`$VAR` or `${VAR}`).

### Global Config

```yaml
opensearch_url: https://opensearch.example.com:9200
ca_cert_path: /certs/ca.crt
insecure: true
credentials:
  - username: "$LOGS_USERNAME"
    password: "$LOGS_PASSWORD"
  - username: "$LOGS2_USERNAME"
    password: "$LOGS2_PASSWORD"
timeout: 30s
max_query_range: 168h

# Optional - cluster stats collectors (all disabled by default)
collect_cluster_health: false
collect_nodes_stats: false
collect_indices_stats: false

queries: []
```

### Query Files

Queries can be in the global config or in separate files loaded via `-queries-dir`:

```yaml
queries:
  - name: error_count
    support_group: observability
    service: logs
    interval: 300s
    indices: "logs-*"
    on_error: preserve
    on_missing: drop
    query:
      size: 0
      query:
        bool:
          must:
            - match: { level: "ERROR" }
            - range: { "@timestamp": { gte: "now-5m" } }
```

### Multi-Team Layout

```
/config/
├── config.yaml          # Connection settings
└── queries/             # Per-team query files
    ├── sre.yaml
    ├── security.yaml
    └── platform.yaml
```

```bash
opensearch-query-exporter -config /config/config.yaml -queries-dir /config/queries/
```

## Query Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `name` | yes | — | Query identifier, used in metric names |
| `support_group` | yes | — | Team owning this query (label on metrics) |
| `service` | yes | — | Service this query monitors (label on metrics) |
| `interval` | no | 300s | How often to run the query |
| `indices` | no | `_all` | OpenSearch indices to query |
| `query` | yes | — | OpenSearch query body |
| `on_error` | no | `drop` | What to do when query fails |
| `on_missing` | no | `drop` | What to do when a metric disappears |
| `metrics` | no | — | Custom metric extraction from response |

## Error Handling Strategies

| Strategy | `on_error` (query fails) | `on_missing` (metric disappears) |
|----------|--------------------------|----------------------------------|
| `preserve` | Keep last successful value | Keep last known value |
| `drop` | Remove all metrics (default) | Remove missing metrics (default) |
| `zero` | Reset values to 0 | Set missing to 0 |

## Max Query Range

Setting `max_query_range: 168h` (7 days) rejects any query with a time range exceeding that limit at startup. This prevents queries from accidentally hitting warm/cold storage tiers.

## Command-Line Options

```
-config string           Configuration file path (default "config.yaml")
-queries-dir string      Directory containing additional query files (*.yaml)
-listen-address string   Metrics server address (default ":9206")
-opensearch-url string   OpenSearch URL override
-insecure               Skip TLS verification
-timeout duration        Query timeout override
-log-level string        Log level: debug, info, warn, error (default "info")
```

## Endpoints

| Path | Description |
|------|-------------|
| `/metrics` | Prometheus metrics |
| `/healthz` | Liveness probe (always 200) |
| `/readyz` | Readiness probe (pings OpenSearch) |

## Generated Metrics

### Always present

- `opensearch_up` - Cluster reachability (1=up, 0=down)

### Per query

- `opensearch_query_{name}_hits` - Total hits from query
- `opensearch_query_{name}_took_milliseconds` - OpenSearch execution time
- `opensearch_query_success{query, support_group, service}` - 1 if last run succeeded
- `opensearch_query_duration_seconds{query, support_group, service}` - End-to-end duration

### Aggregation metrics

Automatically extracted from nested aggregations with bucket keys as labels:

```
opensearch_query_{name}_{agg}_{sub_agg}_doc_count{bucket_key="value"} 42
```

### Optional (disabled by default)

Enable via config:
- `collect_cluster_health: true` - cluster status, shard counts, node counts
- `collect_nodes_stats: true` - per-node JVM, transport, thread pools
- `collect_indices_stats: true` - aggregate index statistics

### Custom Metric Extraction

```yaml
queries:
  - name: custom_metrics
    support_group: observability
    service: api
    query: { ... }
    metrics:
      - name: response_time_p99
        path: aggregations.response_time.values.99.0
        help: "99th percentile response time"
        labels:
          environment: "prod"
        label_paths:
          region: aggregations.by_region.key
```

## Project Structure

```
cmd/exporter/       Main application
pkg/config/         Configuration loading, validation, env expansion
pkg/opensearch/     HTTP client with TLS and credential failover
pkg/metrics/        Prometheus collector with background query execution
pkg/parser/         Response parsing (queries, aggregations, cluster stats)
```
