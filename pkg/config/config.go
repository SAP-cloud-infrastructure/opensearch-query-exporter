package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	OpenSearchURL string        `yaml:"opensearch_url"`
	Credentials   []Credential  `yaml:"credentials"`
	CACertPath    string        `yaml:"ca_cert_path"`
	Insecure      bool          `yaml:"insecure"`
	Timeout       time.Duration `yaml:"timeout"`
	Queries       []Query       `yaml:"queries"`
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
		if err := validateAndSetQueryDefaults(&config.Queries[i], i); err != nil {
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
		queries, err := loadQueriesFile(path)
		if err != nil {
			return fmt.Errorf("failed to load queries from %s: %w", name, err)
		}

		config.Queries = append(config.Queries, queries...)
	}

	return nil
}

// loadQueriesFile loads queries from a single YAML file
func loadQueriesFile(path string) ([]Query, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var qf QueriesFile
	if err := yaml.Unmarshal(data, &qf); err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Validate and set defaults for each query
	for i := range qf.Queries {
		if err := validateAndSetQueryDefaults(&qf.Queries[i], i); err != nil {
			return nil, err
		}
	}

	return qf.Queries, nil
}

// validateAndSetQueryDefaults validates a query and sets default values
func validateAndSetQueryDefaults(q *Query, index int) error {
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
