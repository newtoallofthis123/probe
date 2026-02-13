package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.MaxResultsPerGrep != 30 {
		t.Errorf("expected MaxResultsPerGrep=30, got %d", cfg.MaxResultsPerGrep)
	}
	if cfg.MaxFileReadLines != 200 {
		t.Errorf("expected MaxFileReadLines=200, got %d", cfg.MaxFileReadLines)
	}
	if !cfg.ShowReasons {
		t.Error("expected ShowReasons=true")
	}
}

func TestLoadConfigFile(t *testing.T) {
	// Create temp dir with .probe.toml
	dir := t.TempDir()
	tomlContent := `
model = "gpt-4"
base_url = "https://api.openai.com/v1"
max_turns = 20
max_results_per_grep = 50
max_file_read_lines = 500
show_reasons = false
output_format = "json"
`
	os.WriteFile(filepath.Join(dir, ".probe.toml"), []byte(tomlContent), 0644)

	cf, err := loadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf == nil {
		t.Fatal("expected config file to be loaded")
	}
	if cf.Model != "gpt-4" {
		t.Errorf("model: got %q", cf.Model)
	}
	if cf.MaxResultsPerGrep != 50 {
		t.Errorf("max_results_per_grep: got %d", cf.MaxResultsPerGrep)
	}
	if cf.ShowReasons == nil || *cf.ShowReasons != false {
		t.Error("show_reasons should be false")
	}
}

func TestLoadConfigFileUnknownFields(t *testing.T) {
	dir := t.TempDir()
	tomlContent := `
model = "test"
unknown_field = "should be ignored"
another_unknown = 42
`
	os.WriteFile(filepath.Join(dir, ".probe.toml"), []byte(tomlContent), 0644)

	cf, err := loadConfigFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf.Model != "test" {
		t.Errorf("model: got %q", cf.Model)
	}
}

func TestLoadFileOverlay(t *testing.T) {
	dir := t.TempDir()
	tomlContent := `
model = "custom-model"
max_turns = 5
`
	os.WriteFile(filepath.Join(dir, ".probe.toml"), []byte(tomlContent), 0644)

	cfg := defaultConfig()
	if err := cfg.loadFile(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("model should be overridden, got %q", cfg.Model)
	}
	if cfg.MaxTurns != 5 {
		t.Errorf("max_turns should be overridden, got %d", cfg.MaxTurns)
	}
	// Defaults should be preserved for unset fields
	if cfg.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("base_url should keep default, got %q", cfg.BaseURL)
	}
	if cfg.MaxResultsPerGrep != 30 {
		t.Errorf("max_results_per_grep should keep default, got %d", cfg.MaxResultsPerGrep)
	}
}

func TestLoadFilePrecedence(t *testing.T) {
	// Config file sets model, env overrides it
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".probe.toml"), []byte(`model = "from-file"`), 0644)

	cfg := defaultConfig()
	cfg.loadFile(dir)
	if cfg.Model != "from-file" {
		t.Fatalf("expected from-file, got %q", cfg.Model)
	}

	// Env override
	t.Setenv("PROBE_MODEL", "from-env")
	cfg.loadEnv(map[string]bool{})
	if cfg.Model != "from-env" {
		t.Errorf("env should override file, got %q", cfg.Model)
	}
}

func TestAPIKeyEnv(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".probe.toml"), []byte(`api_key_env = "MY_CUSTOM_KEY"`), 0644)

	t.Setenv("MY_CUSTOM_KEY", "secret-key-123")
	cfg := defaultConfig()
	cfg.loadFile(dir)
	if cfg.APIKey != "secret-key-123" {
		t.Errorf("expected API key from custom env var, got %q", cfg.APIKey)
	}
}

func TestProjectTomlOverridesGlobal(t *testing.T) {
	// Create project dir with .probe.toml
	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, ".probe.toml"), []byte(`model = "project-model"`), 0644)

	// Create global config
	globalDir := t.TempDir()
	os.MkdirAll(filepath.Join(globalDir, "probe"), 0755)
	os.WriteFile(filepath.Join(globalDir, "probe", "config.toml"), []byte(`model = "global-model"`), 0644)
	t.Setenv("XDG_CONFIG_HOME", globalDir)

	cf, err := loadConfigFile(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if cf.Model != "project-model" {
		t.Errorf("project .probe.toml should win over global, got %q", cf.Model)
	}
}
