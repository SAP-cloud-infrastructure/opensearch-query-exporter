# OpenSearch Query Exporter

A Prometheus exporter for OpenSearch queries, written in Go. Teams can define custom queries via YAML configuration and expose results as Prometheus metrics.

## Key Features

- **Team-Oriented**: Independent query ConfigMaps per team
- **Secure**: TLS-only with credential failover support
- **Efficient**: Concurrent query execution with minimal resource usage
- **Resilient**: Configurable error handling strategies (preserve/drop/zero)
- **Comprehensive**: Automatic metric extraction from hits, aggregations, and cluster health

## Quick Start

```bash
# Using Docker
git clone https://github.com/SAP-cloud-infrastructure/opensearch-query-exporter.git
cd opensearch-query-exporter
docker-compose up -d

# View metrics at http://localhost:9206/metrics
```

### Building from Source

```bash
# Build the binary
make build

# Run with example config
./opensearch-query-exporter -config configs/example-config.yaml

# Run with separate queries directory (recommended for multi-team setup)
./opensearch-query-exporter -config configs/global.yaml -queries-dir configs/queries/
```

## Configuration

### Single File Configuration

For simple setups, use a single YAML file with all settings:

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
    on_error: preserve   # keep last value on error
    on_missing: drop     # remove metrics for missing data
    query:
      size: 0
      query:
        bool:
          must:
            - match: { level: "ERROR" }
            - range: { "@timestamp": { gte: "now-5m" } }
```

### Multi-Team Configuration (Recommended)

For production environments with multiple teams, use separate configuration files:

```
/config/
├── global.yaml          # Connection settings (platform team manages)
└── queries/             # Team query files (each team manages their own)
    ├── sre.yaml
    ├── security.yaml
    └── platform.yaml
```

**Kubernetes Deployment:**

```yaml
# Platform team deploys global config
apiVersion: v1
kind: ConfigMap
metadata:
  name: opensearch-exporter-config
data:
  global.yaml: |
    opensearch_url: https://opensearch.example.com:9200
    credentials:
      - username: "${OS_USERNAME}"
        password: "${OS_PASSWORD}"
    ca_cert_path: /etc/ssl/certs/ca.pem
    queries: []

---
# Each team deploys their own queries ConfigMap
apiVersion: v1
kind: ConfigMap
metadata:
  name: opensearch-queries-sre
data:
  sre.yaml: |
    queries:
      - name: error_logs
        team: sre
        interval: 30s
        indices: "logs-*"
        on_error: preserve
        query:
          size: 0
          query:
            bool:
              must:
                - match: { level: "ERROR" }
```

## Error Handling Strategies

Each query supports two error handling settings:

| Strategy | `on_error` (query fails) | `on_missing` (metric disappears) |
|----------|--------------------------|----------------------------------|
| `preserve` | Keep last successful value | Keep last known value |
| `drop` | Remove all metrics (default) | Remove missing metrics (default) |
| `zero` | Reset values to 0 | Set missing to 0 |

**Example:**
```yaml
queries:
  - name: critical_alerts
    team: sre
    on_error: preserve    # Don't lose alerting metrics during outages
    on_missing: zero      # Show explicit 0 when services stop reporting
    # ...
```

## Command-Line Options

```bash
opensearch-query-exporter [OPTIONS]

Options:
  -config string           Configuration file path (default "config.yaml")
  -queries-dir string      Directory containing additional query files (*.yaml)
  -listen-address string   Metrics server address (default ":9206")
  -opensearch-url string   OpenSearch URL (default "https://localhost:9200")
  -insecure               Skip TLS verification
  -timeout duration        Query timeout (default 30s)
  -log-level string        Log level: debug, info, warn, error (default "info")
```

## Generated Metrics

### Automatic Metrics (per query)

- `opensearch_query_{name}_hits_total` - Total hits from query
- `opensearch_query_{name}_took_milliseconds` - Query execution time
- `opensearch_query_success{query="...", team="..."}` - Query success (1) or failure (0)
- `opensearch_query_duration_seconds{query="...", team="..."}` - End-to-end duration

### Cluster Metrics

- `opensearch_up` - Cluster reachability (1=up, 0=down)
- `opensearch_cluster_health_status{cluster="..."}` - Health status (0=green, 1=yellow, 2=red)
- `opensearch_cluster_health_nodes_total{cluster="..."}` - Node count
- `opensearch_cluster_health_shards_total{cluster="...", type="..."}` - Shard counts

### Aggregation Metrics

Automatically extracted from OpenSearch aggregation results with appropriate labels.

## Advanced Configuration

### Custom Metric Extraction

```yaml
queries:
  - name: custom_metrics
    team: platform
    query:
      # Your OpenSearch query with aggregations
    metrics:
      - name: response_time_p99
        path: aggregations.response_time.values.99.0
        help: "99th percentile response time"
        labels:
          service: "api"
        label_paths:
          region: aggregations.by_region.key
```

## Development

### Project Structure

```
.
├── cmd/exporter/          # Main application
├── pkg/
│   ├── config/           # Configuration handling
│   ├── opensearch/       # OpenSearch client
│   ├── metrics/          # Prometheus metrics collection
│   └── parser/           # Response parsing
├── configs/
│   ├── global.yaml       # Example global config
│   ├── example-config.yaml
│   └── queries/          # Example team query files
│       ├── sre.yaml
│       ├── security.yaml
│       └── platform.yaml
└── Dockerfile
```

### Running Tests

```bash
make test
```

### Building

```bash
# Build for current platform
make build

# Build for multiple platforms
make build-all

# Build Docker image
make docker-build
```
