package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic talks to the Claude API directly with an API key, for users who do
// not have the Claude Code CLI installed (or who prefer the API backend).
type Anthropic struct {
	APIKey string
	Model  string // alias (opus/sonnet/haiku) or full id; empty = default
}

// Name implements Provider.
func (a *Anthropic) Name() string { return "anthropic-api (" + a.modelID() + ")" }

// modelID maps a user-friendly alias to a full API model id, defaulting to the
// most capable model. A value that already looks like a full id (anything not
// matching an alias) passes through unchanged.
func (a *Anthropic) modelID() string {
	switch strings.ToLower(strings.TrimSpace(a.Model)) {
	case "", "opus":
		return string(anthropic.ModelClaudeOpus4_8)
	case "sonnet":
		return string(anthropic.ModelClaudeSonnet4_6)
	case "haiku":
		return string(anthropic.ModelClaudeHaiku4_5_20251001)
	default:
		return a.Model
	}
}

// Analyze sends the instruction as the system prompt and the evidence as the
// user turn, returning the model's text answer. Adaptive thinking is enabled so
// the model reasons before answering (root-cause analysis benefits from it);
// thinking blocks are skipped when collecting the answer text.
func (a *Anthropic) Analyze(ctx context.Context, instruction, evidence string) (string, error) {
	client := anthropic.NewClient(option.WithAPIKey(a.APIKey))

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.modelID()),
		MaxTokens: 16000,
		System:    []anthropic.TextBlockParam{{Text: instruction}},
		Thinking: anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(evidence)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic api: %w", err)
	}

	var sb strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("anthropic api returned no text content")
	}
	return out, nil
}
