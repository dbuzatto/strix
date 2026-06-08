package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
	nameStyle   = lipgloss.NewStyle().Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
)

// sparkBlocks are eight increasing heights used to draw a sparkline.
var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// styleFor picks a color by utilization: green < 50% < yellow < 80% < red.
// A negative percent (unknown total, e.g. pods) falls back to blue.
func styleFor(pct float64) lipgloss.Style {
	switch {
	case pct < 0:
		return blue
	case pct < 50:
		return green
	case pct < 80:
		return yellow
	default:
		return red
	}
}

// gauge renders a colored htop-style bar, or a placeholder when % is unknown.
func gauge(pct float64) string {
	if pct < 0 {
		return dimStyle.Render("[ no limit ]") + "     "
	}
	const width = 12
	filled := int(pct/100*width + 0.5)
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat(" ", width-filled)
	return "[" + styleFor(pct).Render(bar) + "] " + fmt.Sprintf("%3.0f%%", pct)
}

// spark renders the value history as a colored sparkline. When pct is known the
// scale is 0..100; otherwise it auto-scales to the window's own maximum.
func spark(vals []float64, pct float64) string {
	if len(vals) == 0 {
		return ""
	}
	max := 100.0
	if pct < 0 {
		max = 1
		for _, v := range vals {
			if v > max {
				max = v
			}
		}
	}
	var sb strings.Builder
	for _, v := range vals {
		idx := int(v / max * float64(len(sparkBlocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		sb.WriteRune(sparkBlocks[idx])
	}
	return styleFor(pct).Render(sb.String())
}

// fmtCPU prints millicores the way kubectl top does, e.g. "1719m".
func fmtCPU(milli int64) string { return fmt.Sprintf("%dm", milli) }

// fmtMem prints bytes as a human-friendly Mi/Gi value.
func fmtMem(bytes int64) string {
	const mi = 1024 * 1024
	if bytes >= 1024*mi {
		return fmt.Sprintf("%.1fGi", float64(bytes)/float64(1024*mi))
	}
	return fmt.Sprintf("%dMi", bytes/mi)
}
