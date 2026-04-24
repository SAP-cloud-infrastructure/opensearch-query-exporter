# OpenSearch Query Exporter

A Prometheus exporter for OpenSearch queries, written in Go. Teams define custom queries via YAML configuration and expose results as Prometheus metrics.

## Features

- **Team-Oriented**: Independent query ConfigMaps per team
- **Secure**: TLS-only with credential failover support
- **Efficient**: Concurrent query execution with minimal resource usage
- **Resilient**: Configurable error handling strategies (preserve/drop/zero)
- **Comprehensive**: Automatic metric extraction from hits, aggregations, and cluster health

## Building

```bash
make build
make test
```

## Configuration

### Single File

```yaml
opensearch_url: https://opensearch.example.com:9200
credentials:
  - username: "primary_user"
    password: "primary_password"
ca_cert_path: /etc/ssl/certs/opensearch-ca.pem

queries:
  - name: error_count
    team: sre
    interval: 60s
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

### Multi-Team (Recommended)

Split configuration into a global file (connection settings) and per-team query files:

```
/config/
├── global.yaml          # Connection settings (platform team manages)
└── queries/             # Team query files (each team manages their own)
    ├── sre.yaml
    ├── security.yaml
    └── platform.yaml
```

Run with:

```bash
opensearch-query-exporter -config /config/global.yaml -queries-dir /config/queries/
```

## Error Handling Strategies

Each query supports two error handling settings:

| Strategy | `on_error` (query fails) | `on_missing` (metric disappears) |
|----------|--------------------------|----------------------------------|
| `preserve` | Keep last successful value | Keep last known value |
| `drop` | Remove all metrics (default) | Remove missing metrics (default) |
| `zero` | Reset values to 0 | Set missing to 0 |

## Command-Line Options

```
-config string           Configuration file path (default "config.yaml")
-queries-dir string      Directory containing additional query files (*.yaml)
-listen-address string   Metrics server address (default ":9206")
-opensearch-url string   OpenSearch URL (default "https://localhost:9200")
-insecure               Skip TLS verification
-timeout duration        Query timeout (default 30s)
-log-level string        Log level: debug, info, warn, error (default "info")
```

## Endpoints

| Path | Description |
|------|-------------|
| `/metrics` | Prometheus metrics |
| `/healthz` | Liveness probe (always 200) |
| `/readyz` | Readiness probe (pings OpenSearch) |

## Generated Metrics

### Per Query

- `opensearch_query_{name}_hits_total` - Total hits
- `opensearch_query_{name}_took_milliseconds` - Query execution time
- `opensearch_query_success{query, team}` - Query success (1) or failure (0)
- `opensearch_query_duration_seconds{query, team}` - End-to-end duration

### Cluster

- `opensearch_up` - Cluster reachability
- `opensearch_cluster_health_status{cluster}` - Health status (0=green, 1=yellow, 2=red)
- `opensearch_cluster_health_*` - Node counts, shard counts, etc.
- `opensearch_nodes_stats_*{node_id, node_name}` - Per-node statistics
- `opensearch_indices_stats_*` - Aggregate index statistics

### Custom Metric Extraction

```yaml
queries:
  - name: custom_metrics
    team: platform
    query: { ... }
    metrics:
      - name: response_time_p99
        path: aggregations.response_time.values.99.0
        help: "99th percentile response time"
        labels:
          service: "api"
```

## Project Structure

```
cmd/exporter/       Main application
pkg/config/         Configuration loading and validation
pkg/opensearch/     OpenSearch HTTP client with TLS and credential failover
pkg/metrics/        Prometheus collector with background query execution
pkg/parser/         Response parsing (queries, cluster health, nodes, indices)
configs/            Example configuration files
```
