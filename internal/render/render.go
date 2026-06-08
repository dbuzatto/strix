// Package render turns the AI's Markdown answer into ANSI-styled text for the
// terminal. Terminals understand ANSI escape codes, not Markdown, so raw
// output like **bold** must be converted before it looks formatted.
package render

import (
	"os"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// IsTTY reports whether stdout is an interactive terminal. When it is not
// (a pipe or a file), callers should emit raw Markdown so the bytes stay clean.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Markdown renders Markdown to ANSI-styled text wrapped to the terminal width.
// The style adapts to the terminal's light/dark background.
func Markdown(s string) (string, error) {
	width := 100
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return s, err
	}
	return r.Render(s)
}
