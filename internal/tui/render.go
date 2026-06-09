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

// usedOfLimit renders "used / limit"; when the limit is unknown (no limit set)
// it shows just the used value, since there is no ceiling to compare against.
func usedOfLimit(used, limit string, hasLimit bool) string {
	if hasLimit {
		return used + " / " + limit
	}
	return used
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
