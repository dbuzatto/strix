package ai

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeCode runs analysis through the locally installed Claude Code CLI in
// headless mode (`claude -p`), reusing whatever auth the user already has.
type ClaudeCode struct {
	Bin   string // resolved path to the claude binary
	Model string // optional model alias or id; empty = Claude Code's default
}

// Name implements Provider.
func (c *ClaudeCode) Name() string {
	if c.Model != "" {
		return "claude-code (" + c.Model + ")"
	}
	return "claude-code"
}

// Analyze pipes the evidence to `claude -p <instruction>` on stdin. Keeping the
// evidence on stdin avoids argument-length limits for large logs.
func (c *ClaudeCode) Analyze(ctx context.Context, instruction, evidence string) (string, error) {
	prompt := instruction + "\n\nThe Kubernetes evidence to analyze is provided on stdin."

	args := []string{"-p", prompt}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}

	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Stdin = strings.NewReader(evidence)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude code failed: %s", msg)
	}

	return strings.TrimSpace(stdout.String()), nil
}
