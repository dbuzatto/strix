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
	labelStyle  = lipgloss.NewStyle().Faint(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
)

const gaugeWidth = 10

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

// gauge renders a colored level bar inside a thin bracket with the percentage,
// e.g. "▕████····▏ 76%". The empty track is shown with dim dots; an unknown
// total (no limit) keeps the same width and shows "n/a".
func gauge(pct float64) string {
	if pct < 0 {
		track := dimStyle.Render(strings.Repeat("·", gaugeWidth))
		return "▕" + track + "▏" + dimStyle.Render("  n/a")
	}
	filled := int(pct/100*gaugeWidth + 0.5)
	if filled > gaugeWidth {
		filled = gaugeWidth
	}
	bar := styleFor(pct).Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("·", gaugeWidth-filled))
	return "▕" + bar + "▏" + fmt.Sprintf(" %3.0f%%", pct)
}

// spark renders the value history as a colored sparkline scaled between the
// window's own min and max, so it reads as a trend graph (the gauge already
// shows the absolute level). Steady usage shows as a flat low line. Color
// follows the current utilization.
func spark(vals []float64, pct float64) string {
	if len(vals) == 0 {
		return ""
	}

	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	span := hi - lo

	var sb strings.Builder
	for _, v := range vals {
		idx := 0
		if span > 0 {
			idx = int((v - lo) / span * float64(len(sparkBlocks)-1))
		}
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
