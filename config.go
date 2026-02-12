package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Model        string
	BaseURL      string
	APIKey       string
	MaxTurns     int
	ProjectDir   string
	OutputFormat string // "human", "json", "paths"
	Verbose      bool
	Quiet        bool
}

func defaultConfig() Config {
	return Config{
		Model:        "ministral-3:3b",
		BaseURL:      "http://localhost:11434/v1",
		MaxTurns:     10,
		ProjectDir:   ".",
		OutputFormat: "human",
	}
}

// loadEnv overlays environment variables onto the config.
// Only sets values that weren't explicitly set by flags.
func (c *Config) loadEnv(flagSet map[string]bool) {
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

	// TODO: load config file (.probe.toml in project root, then $XDG_CONFIG_HOME/probe/config.toml)
	// Precedence: flags > env > config file > defaults
}

// resolveProjectDir resolves ProjectDir to an absolute path and validates it.
func (c *Config) resolveProjectDir() error {
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
