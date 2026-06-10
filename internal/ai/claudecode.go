package ai

import (
	"bytes"
	"context"
	"errors"
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
		// A timeout is the most common failure on large evidence; name it
		// clearly so the user knows to lower --tail or rerun.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("claude code timed out — try a smaller --tail, or rerun")
		}
		// Claude reports some failures (usage limits, auth, API errors) on
		// stdout rather than stderr, so surface both before the bare exit
		// status — otherwise the user just sees "exit status 1".
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		// Oversized evidence is recoverable: point the user at the knob.
		if strings.Contains(strings.ToLower(detail), "too long") {
			return "", fmt.Errorf("claude code failed: %s — the evidence is too large for the model; rerun with a smaller --tail (e.g. --tail 20)", detail)
		}
		return "", fmt.Errorf("claude code failed: %s", detail)
	}

	return strings.TrimSpace(stdout.String()), nil
}
