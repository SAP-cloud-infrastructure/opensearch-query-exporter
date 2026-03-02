package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

func TestLoadConfig_Success_Insecure(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
timeout: 5s
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if cfg.OpenSearchURL == "" || len(cfg.Credentials) != 1 || !cfg.Insecure {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadConfig_Fail_HTTP_URL(t *testing.T) {
	yaml := `
opensearch_url: http://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("expected error for non-https opensearch_url")
	}
}

func TestLoadConfig_Fail_NoCredentials(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
insecure: true
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("expected error for missing credentials")
	}
}

func TestLoadConfig_Fail_EmptyCredentialFields(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: ""
    password: pass
insecure: true
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("expected error for empty username")
	}
}

func TestLoadConfig_ErrorStrategies_Defaults(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
queries:
  - name: q1
    team: t1
    query:
      size: 0
`
	path := writeTempConfig(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Queries[0].OnError != StrategyDrop {
		t.Errorf("expected default OnError=drop, got %s", cfg.Queries[0].OnError)
	}
	if cfg.Queries[0].OnMissing != StrategyDrop {
		t.Errorf("expected default OnMissing=drop, got %s", cfg.Queries[0].OnMissing)
	}
}

func TestLoadConfig_ErrorStrategies_Custom(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
queries:
  - name: q1
    team: t1
    on_error: preserve
    on_missing: zero
    query:
      size: 0
`
	path := writeTempConfig(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Queries[0].OnError != StrategyPreserve {
		t.Errorf("expected OnError=preserve, got %s", cfg.Queries[0].OnError)
	}
	if cfg.Queries[0].OnMissing != StrategyZero {
		t.Errorf("expected OnMissing=zero, got %s", cfg.Queries[0].OnMissing)
	}
}

func TestLoadConfig_ErrorStrategies_Invalid(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
queries:
  - name: q1
    team: t1
    on_error: invalid_strategy
    query:
      size: 0
`
	path := writeTempConfig(t, yaml)
	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("expected error for invalid on_error strategy")
	}
}

func TestLoadQueriesDir_Success(t *testing.T) {
	// Create base config
	baseYaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
queries: []
`
	basePath := writeTempConfig(t, baseYaml)
	cfg, err := LoadConfig(basePath)
	if err != nil {
		t.Fatalf("failed to load base config: %v", err)
	}

	// Create queries directory with team files
	queriesDir := filepath.Join(t.TempDir(), "queries")
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatalf("failed to create queries dir: %v", err)
	}

	team1Yaml := `
queries:
  - name: team1_query
    team: team1
    query:
      size: 0
`
	team2Yaml := `
queries:
  - name: team2_query
    team: team2
    on_error: preserve
    query:
      size: 0
`
	if err := os.WriteFile(filepath.Join(queriesDir, "team1.yaml"), []byte(team1Yaml), 0o600); err != nil {
		t.Fatalf("failed to write team1.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(queriesDir, "team2.yaml"), []byte(team2Yaml), 0o600); err != nil {
		t.Fatalf("failed to write team2.yaml: %v", err)
	}

	// Load queries directory
	if err := LoadQueriesDir(cfg, queriesDir); err != nil {
		t.Fatalf("failed to load queries dir: %v", err)
	}

	if len(cfg.Queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(cfg.Queries))
	}

	// Verify queries were loaded
	foundTeam1, foundTeam2 := false, false
	for _, q := range cfg.Queries {
		if q.Name == "team1_query" && q.Team == "team1" {
			foundTeam1 = true
		}
		if q.Name == "team2_query" && q.Team == "team2" && q.OnError == StrategyPreserve {
			foundTeam2 = true
		}
	}
	if !foundTeam1 || !foundTeam2 {
		t.Fatalf("not all team queries found: team1=%v, team2=%v", foundTeam1, foundTeam2)
	}
}

func TestLoadQueriesDir_NonExistent(t *testing.T) {
	cfg := &Config{}
	// Should not error on non-existent directory
	if err := LoadQueriesDir(cfg, "/nonexistent/path"); err != nil {
		t.Fatalf("unexpected error for non-existent dir: %v", err)
	}
}

func TestLoadQueriesDir_SkipsNonYaml(t *testing.T) {
	cfg := &Config{}
	dir := t.TempDir()

	// Create a non-yaml file
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not yaml"), 0o600); err != nil {
		t.Fatalf("failed to write txt file: %v", err)
	}
	// Create a yaml file
	yamlContent := `
queries:
  - name: test
    team: test
    query:
      size: 0
`
	if err := os.WriteFile(filepath.Join(dir, "queries.yaml"), []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write yaml file: %v", err)
	}

	if err := LoadQueriesDir(cfg, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(cfg.Queries))
	}
}

func TestLoadQueriesDir_InvalidYaml(t *testing.T) {
	cfg := &Config{}
	dir := t.TempDir()

	// Create invalid yaml file
	if err := os.WriteFile(filepath.Join(dir, "invalid.yaml"), []byte("not: valid: yaml: ["), 0o600); err != nil {
		t.Fatalf("failed to write invalid yaml: %v", err)
	}

	if err := LoadQueriesDir(cfg, dir); err == nil {
		t.Fatalf("expected error for invalid yaml")
	}
}

func TestLoadQueriesDir_MissingRequiredFields(t *testing.T) {
	cfg := &Config{}
	dir := t.TempDir()

	// Create yaml with missing required team field
	yamlContent := `
queries:
  - name: test
    query:
      size: 0
`
	if err := os.WriteFile(filepath.Join(dir, "invalid.yaml"), []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	if err := LoadQueriesDir(cfg, dir); err == nil {
		t.Fatalf("expected error for missing team field")
	}
}
