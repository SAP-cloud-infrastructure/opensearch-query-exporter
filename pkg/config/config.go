package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrorStrategy defines how to handle errors or missing metrics
type ErrorStrategy string

const (
	// StrategyPreserve keeps metrics/values from the last successful run
	StrategyPreserve ErrorStrategy = "preserve"
	// StrategyDrop removes metrics previously produced by the query
	StrategyDrop ErrorStrategy = "drop"
	// StrategyZero keeps metrics but resets their values to 0
	StrategyZero ErrorStrategy = "zero"
)

// Config represents the main configuration structure
type Config struct {
	OpenSearchURL  string        `yaml:"opensearch_url"`
	Credentials    []Credential  `yaml:"credentials"`
	CACertPath     string        `yaml:"ca_cert_path"`
	Insecure       bool          `yaml:"insecure"`
	Timeout        time.Duration `yaml:"timeout"`
	MaxQueryRange  time.Duration `yaml:"max_query_range"`
	Queries        []Query       `yaml:"queries"`
}

// Credential represents a set of authentication credentials
type Credential struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Query represents a team's query configuration
type Query struct {
	Name        string          `yaml:"name"`
	Team        string          `yaml:"team"`
	Description string          `yaml:"description"`
	Interval    time.Duration   `yaml:"interval"`
	Indices     string          `yaml:"indices"`
	Query       map[string]any  `yaml:"query"`
	Metrics     []MetricMapping `yaml:"metrics"`
	OnError     ErrorStrategy   `yaml:"on_error"`
	OnMissing   ErrorStrategy   `yaml:"on_missing"`
}

// MetricMapping defines how to extract metrics from query results
type MetricMapping struct {
	Name       string            `yaml:"name"`
	Path       string            `yaml:"path"`
	Labels     map[string]string `yaml:"labels"`
	LabelPaths map[string]string `yaml:"label_paths"`
	Help       string            `yaml:"help"`
}

// QueriesFile represents a file containing only queries (for team ConfigMaps)
type QueriesFile struct {
	Queries []Query `yaml:"queries"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in config (e.g. $OS_PASSWORD or ${OS_PASSWORD})
	data = []byte(os.ExpandEnv(string(data)))

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if config.OpenSearchURL == "" {
		config.OpenSearchURL = "https://localhost:9200"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// Enforce HTTPS only
	if !strings.HasPrefix(strings.ToLower(config.OpenSearchURL), "https://") {
		return nil, fmt.Errorf("opensearch_url must use https scheme")
	}

	// Validate credentials
	if len(config.Credentials) == 0 {
		return nil, fmt.Errorf("at least one credential pair must be provided")
	}

	// Validate each credential
	for i, cred := range config.Credentials {
		if cred.Username == "" || cred.Password == "" {
			return nil, fmt.Errorf("credential %d: both username and password must be provided", i+1)
		}
	}

	// Validate and set defaults for queries
	for i := range config.Queries {
		if err := validateAndSetQueryDefaults(&config.Queries[i], i, config.MaxQueryRange); err != nil {
			return nil, err
		}
	}

	return &config, nil
}

// LoadQueriesDir loads all query files from a directory and merges them into the config
func LoadQueriesDir(config *Config, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist, that's okay - no additional queries
			return nil
		}
		return fmt.Errorf("failed to read queries directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		queries, err := loadQueriesFile(path, config.MaxQueryRange)
		if err != nil {
			return fmt.Errorf("failed to load queries from %s: %w", name, err)
		}

		config.Queries = append(config.Queries, queries...)
	}

	return nil
}

// loadQueriesFile loads queries from a single YAML file
func loadQueriesFile(path string, maxRange time.Duration) ([]Query, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	data = []byte(os.ExpandEnv(string(data)))

	var qf QueriesFile
	if err := yaml.Unmarshal(data, &qf); err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Validate and set defaults for each query
	for i := range qf.Queries {
		if err := validateAndSetQueryDefaults(&qf.Queries[i], i, maxRange); err != nil {
			return nil, err
		}
	}

	return qf.Queries, nil
}

// validateAndSetQueryDefaults validates a query and sets default values
func validateAndSetQueryDefaults(q *Query, index int, maxRange time.Duration) error {
	if q.Name == "" {
		return fmt.Errorf("query #%d: name is required", index+1)
	}
	if q.Team == "" {
		return fmt.Errorf("query %s: team is required", q.Name)
	}
	if q.Interval == 0 {
		q.Interval = 60 * time.Second
	}
	if q.Indices == "" {
		q.Indices = "_all"
	}
	if q.Query == nil {
		return fmt.Errorf("query %s: query body is required", q.Name)
	}

	// Validate time ranges in query body
	if maxRange > 0 {
		if err := validateQueryRange(q.Query, maxRange); err != nil {
			return fmt.Errorf("query %s: %w", q.Name, err)
		}
	}

	// Set default error strategies
	if q.OnError == "" {
		q.OnError = StrategyDrop
	}
	if q.OnMissing == "" {
		q.OnMissing = StrategyDrop
	}

	// Validate error strategies
	if !isValidStrategy(q.OnError) {
		return fmt.Errorf("query %s: invalid on_error strategy %q (must be preserve, drop, or zero)", q.Name, q.OnError)
	}
	if !isValidStrategy(q.OnMissing) {
		return fmt.Errorf("query %s: invalid on_missing strategy %q (must be preserve, drop, or zero)", q.Name, q.OnMissing)
	}

	return nil
}

// isValidStrategy checks if a strategy is valid
func isValidStrategy(s ErrorStrategy) bool {
	return s == StrategyPreserve || s == StrategyDrop || s == StrategyZero
}

// nowOffsetRe matches OpenSearch date math expressions like "now-5m", "now-30d", "now-1h".
var nowOffsetRe = regexp.MustCompile(`^now-(\d+)([smhdw])$`)

// validateQueryRange walks the query body and rejects any time range exceeding maxRange.
func validateQueryRange(query map[string]any, maxRange time.Duration) error {
	var walk func(v any) error
	walk = func(v any) error {
		switch val := v.(type) {
		case map[string]any:
			// Check if this is a "range" clause with a timestamp field
			if rangeBody, ok := val["range"]; ok {
				if err := checkRangeClause(rangeBody, maxRange); err != nil {
					return err
				}
			}
			for _, child := range val {
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range val {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(query)
}

// checkRangeClause inspects a range clause for timestamp fields that exceed maxRange.
func checkRangeClause(v any, maxRange time.Duration) error {
	rangeMap, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for field, constraints := range rangeMap {
		if !isTimestampField(field) {
			continue
		}
		cMap, ok := constraints.(map[string]any)
		if !ok {
			continue
		}
		for op, expr := range cMap {
			if op != "gte" && op != "gt" && op != "lte" && op != "lt" {
				continue
			}
			s, ok := expr.(string)
			if !ok {
				continue
			}
			d, ok := parseNowOffset(s)
			if !ok {
				continue
			}
			if d > maxRange {
				return fmt.Errorf("range %q: %q exceeds max_query_range (%s)", field, s, maxRange)
			}
		}
	}
	return nil
}

// isTimestampField returns true for common timestamp field names.
func isTimestampField(field string) bool {
	return field == "@timestamp" || field == "timestamp" || strings.HasSuffix(field, ".timestamp")
}

// parseNowOffset parses "now-5m", "now-7d" etc. and returns the offset duration.
func parseNowOffset(s string) (time.Duration, bool) {
	m := nowOffsetRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "s":
		return time.Duration(n) * time.Second, true
	case "m":
		return time.Duration(n) * time.Minute, true
	case "h":
		return time.Duration(n) * time.Hour, true
	case "d":
		return time.Duration(n) * 24 * time.Hour, true
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, true
	}
	return 0, false
}
