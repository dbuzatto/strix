// Package ai provides pluggable AI backends for analysis, preferring a local
// Claude Code install so the user needs no extra API key, and falling back to
// the Anthropic API when a key is configured.
package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Provider analyzes Kubernetes evidence and returns a human-readable explanation.
type Provider interface {
	// Name identifies the backend, e.g. "claude-code".
	Name() string
	// Analyze sends an instruction plus evidence and returns the model's answer.
	Analyze(ctx context.Context, instruction, evidence string) (string, error)
}

// Options configure the chosen backend.
type Options struct {
	// Model selects the model to use (e.g. "opus", "sonnet", a full id).
	// Empty means the backend's own default.
	Model string
	// APIKey is an Anthropic API key from the config file. When empty, the
	// ANTHROPIC_API_KEY environment variable is used instead.
	APIKey string
	// Backend forces a specific backend: "claude-code", "api", or "auto"/""
	// (prefer Claude Code, fall back to the API key).
	Backend string
}

// Detect picks an AI backend honoring the configured Backend, otherwise in
// priority order:
//  1. a local Claude Code install (`claude` on PATH) — uses the user's own auth
//  2. an Anthropic API key (config api_key, else ANTHROPIC_API_KEY) — direct API
//
// It returns an error if the selected (or any) backend is unavailable.
func Detect(opts Options) (Provider, error) {
	key := opts.APIKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	claudePath, claudeErr := exec.LookPath("claude")
	hasClaude := claudeErr == nil

	switch strings.ToLower(strings.TrimSpace(opts.Backend)) {
	case "claude-code":
		if !hasClaude {
			return nil, fmt.Errorf("backend 'claude-code' selected but the 'claude' CLI is not on PATH")
		}
		return &ClaudeCode{Bin: claudePath, Model: opts.Model}, nil
	case "api":
		if key == "" {
			return nil, fmt.Errorf("backend 'api' selected but no API key found (set api_key in the config file or ANTHROPIC_API_KEY)")
		}
		return &Anthropic{APIKey: key, Model: opts.Model}, nil
	case "", "auto":
		if hasClaude {
			return &ClaudeCode{Bin: claudePath, Model: opts.Model}, nil
		}
		if key != "" {
			return &Anthropic{APIKey: key, Model: opts.Model}, nil
		}
		return nil, fmt.Errorf("no AI backend found: install Claude Code (the 'claude' CLI), or set api_key in the config file or ANTHROPIC_API_KEY")
	default:
		return nil, fmt.Errorf("unknown backend %q (use 'auto', 'claude-code', or 'api')", opts.Backend)
	}
}
