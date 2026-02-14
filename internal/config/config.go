package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Model             string
	BaseURL           string
	APIKey            string
	MaxTurns          int
	ProjectDir        string
	OutputFormat      string // "human", "json", "paths"
	Verbose           bool
	Quiet             bool
	MaxResultsPerGrep int
	MaxFileReadLines  int
	ShowReasons       bool
	Think             bool
	Mode              string // "auto", "locate", "explore", "trace"
	Provider          string // "openai", "anthropic", "google" (auto-detected if empty)
}

func DefaultConfig() Config {
	return Config{
		Model:             "ministral-3:3b",
		BaseURL:           "http://localhost:11434/v1",
		MaxTurns:          10,
		ProjectDir:        ".",
		OutputFormat:      "human",
		MaxResultsPerGrep: 30,
		MaxFileReadLines:  200,
		ShowReasons:       true,
		Mode:              "auto",
	}
}

// LoadEnv overlays environment variables onto the config.
// Only sets values that weren't explicitly set by flags.
func (c *Config) LoadEnv(flagSet map[string]bool) {
	if !flagSet["model"] {
		if v := os.Getenv("PROBE_MODEL"); v != "" {
			c.Model = v
		}
	}
	if !flagSet["base-url"] {
		if v := os.Getenv("PROBE_BASE_URL"); v != "" {
			c.BaseURL = v
		}
	}
	if !flagSet["max-turns"] {
		if v := os.Getenv("PROBE_MAX_TURNS"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.MaxTurns = n
			}
		}
	}
	if v := os.Getenv("PROBE_API_KEY"); v != "" {
		c.APIKey = v
	}
	if !flagSet["mode"] {
		if v := os.Getenv("PROBE_MODE"); v != "" {
			c.Mode = v
		}
	}
	if !flagSet["provider"] {
		if v := os.Getenv("PROBE_PROVIDER"); v != "" {
			c.Provider = v
		}
	}
	if !flagSet["think"] {
		if v := os.Getenv("PROBE_THINK"); v != "" {
			c.Think = v == "1" || v == "true"
		}
	}
}

// ResolveProjectDir resolves ProjectDir to an absolute path and validates it.
func (c *Config) ResolveProjectDir() error {
	abs, err := filepath.Abs(c.ProjectDir)
	if err != nil {
		return fmt.Errorf("resolving directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("directory '%s' does not exist", abs)
	}
	if !info.IsDir() {
		return fmt.Errorf("'%s' is not a directory", abs)
	}
	c.ProjectDir = abs
	return nil
}

// configFile represents the TOML config file schema.
type configFile struct {
	Model             string `toml:"model"`
	BaseURL           string `toml:"base_url"`
	APIKeyEnv         string `toml:"api_key_env"`
	MaxTurns          int    `toml:"max_turns"`
	MaxResultsPerGrep int    `toml:"max_results_per_grep"`
	MaxFileReadLines  int    `toml:"max_file_read_lines"`
	ShowReasons       *bool  `toml:"show_reasons"`
	OutputFormat      string `toml:"output_format"`
	Think             *bool  `toml:"think"`
	Mode              string `toml:"mode"`
	Provider          string `toml:"provider"`
}

// loadConfigFile searches for a config file in standard locations.
// Order: .probe.toml in projectDir, then XDG_CONFIG_HOME/probe/config.toml,
// then ~/.config/probe/config.toml. First found wins.
func loadConfigFile(projectDir string) (*configFile, error) {
	candidates := []string{
		filepath.Join(projectDir, ".probe.toml"),
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "probe", "config.toml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "probe", "config.toml"))
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			var cf configFile
			if _, err := toml.DecodeFile(path, &cf); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", path, err)
			}
			return &cf, nil
		}
	}
	return nil, nil // no config file found
}

// LoadFile overlays config file values onto Config. Only non-zero values override.
func (c *Config) LoadFile(projectDir string) error {
	cf, err := loadConfigFile(projectDir)
	if err != nil {
		return err
	}
	if cf == nil {
		return nil
	}
	if cf.Model != "" {
		c.Model = cf.Model
	}
	if cf.BaseURL != "" {
		c.BaseURL = cf.BaseURL
	}
	if cf.APIKeyEnv != "" {
		if v := os.Getenv(cf.APIKeyEnv); v != "" {
			c.APIKey = v
		}
	}
	if cf.MaxTurns > 0 {
		c.MaxTurns = cf.MaxTurns
	}
	if cf.MaxResultsPerGrep > 0 {
		c.MaxResultsPerGrep = cf.MaxResultsPerGrep
	}
	if cf.MaxFileReadLines > 0 {
		c.MaxFileReadLines = cf.MaxFileReadLines
	}
	if cf.ShowReasons != nil {
		c.ShowReasons = *cf.ShowReasons
	}
	if cf.OutputFormat != "" {
		c.OutputFormat = cf.OutputFormat
	}
	if cf.Think != nil {
		c.Think = *cf.Think
	}
	if cf.Mode != "" {
		c.Mode = cf.Mode
	}
	if cf.Provider != "" {
		c.Provider = cf.Provider
	}
	return nil
}
