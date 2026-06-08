// Package ai provides pluggable AI backends for analysis, preferring a local
// Claude Code install so the user needs no extra API key.
package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
}

// Detect picks an AI backend in priority order:
//  1. a local Claude Code install (`claude` on PATH) — uses the user's own auth
//  2. an Anthropic API key (ANTHROPIC_API_KEY) — direct API, not yet implemented
//
// It returns an error if no backend is available.
func Detect(opts Options) (Provider, error) {
	if path, err := exec.LookPath("claude"); err == nil {
		return &ClaudeCode{Bin: path, Model: opts.Model}, nil
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return nil, fmt.Errorf("found ANTHROPIC_API_KEY but the direct API backend is not implemented yet; install Claude Code for now")
	}
	return nil, fmt.Errorf("no AI backend found: install Claude Code (the 'claude' CLI) or set ANTHROPIC_API_KEY")
}
