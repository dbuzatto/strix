package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
)

// TestMarkdownProducesANSI verifies that Markdown is actually converted to ANSI
// escape sequences rather than left as literal **bold** text. AutoStyle would
// fall back to a no-color style under `go test` (no TTY), so force a styled
// renderer to assert the conversion path works.
func TestMarkdownProducesANSI(t *testing.T) {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	out, err := r.Render("**bold** and `code`")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out, "**bold**") {
		t.Errorf("markdown was not converted, still literal: %q", out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI escape codes in output, got: %q", out)
	}
}

// TestMarkdownNoError ensures the production helper does not error on typical input.
func TestMarkdownNoError(t *testing.T) {
	if _, err := Markdown("# Title\n\nSome **text**."); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
}
