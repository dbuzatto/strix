// Package config loads Strix's optional user config file, which holds defaults
// (model, language, …) that command-line flags can override.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// Config holds user defaults. Field tags are JSON because sigs.k8s.io/yaml
// decodes YAML through JSON.
type Config struct {
	Model   string `json:"model"`   // default Claude model (opus, sonnet, haiku, or a full id)
	Lang    string `json:"lang"`    // default answer language (e.g. pt, en)
	Tail    int64  `json:"tail"`    // default log lines gathered per container
	APIKey  string `json:"api_key"` // optional Anthropic API key for the direct API backend
	Backend string `json:"backend"` // AI backend: auto (default), claude-code, or api
}

// Path returns the config file location, honoring $STRIX_CONFIG, then
// $XDG_CONFIG_HOME, falling back to ~/.config/strix/config.yaml.
func Path() string {
	if p := os.Getenv("STRIX_CONFIG"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "strix", "config.yaml")
}

// Load reads the config file. A missing file is not an error — it returns a
// zero-value Config so callers can always fall back to their own defaults.
func Load() (Config, error) {
	var cfg Config
	path := Path()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, nil
}

// sample is the commented config written by `strix config init`.
const sample = `# Strix configuration — defaults for all commands.
# Any flag passed on the command line overrides these values.

# Default Claude model: opus, sonnet, haiku, or a full id.
# Empty means use whatever your Claude Code is set to.
model: ""

# Default language for AI answers (e.g. pt, en, "português").
# Empty means let the model decide.
lang: ""

# Log lines gathered per container during analysis.
tail: 100

# AI backend: auto (default), claude-code, or api.
#   auto        prefer the local Claude Code CLI, fall back to the API key
#   claude-code force the local 'claude' CLI
#   api         force the direct Anthropic API (requires api_key or ANTHROPIC_API_KEY)
backend: auto

# Anthropic API key for the direct API backend. Leave empty to use the
# ANTHROPIC_API_KEY environment variable instead. Keep this file private.
api_key: ""
`

// WriteSample writes the commented default config to Path(), creating parent
// directories. It refuses to overwrite an existing file unless force is true.
func WriteSample(force bool) (string, error) {
	path := Path()
	if path == "" {
		return "", fmt.Errorf("could not resolve config path (no HOME or XDG_CONFIG_HOME)")
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return path, fmt.Errorf("config already exists at %s (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// 0600: the file may hold an API key, so keep it readable only by the user.
	if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
