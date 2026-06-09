// Package tui renders the live `strix top --watch` dashboard: an htop-style
// view that polls metrics and draws real-time usage gauges in the alternate
// screen, returning the terminal to the shell on quit.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dbuzatto/strix/internal/k8s"
)

// FetchFunc returns the current usage sample (e.g. nodes or pods).
type FetchFunc func(context.Context) ([]k8s.Usage, error)

const (
	interval     = 2 * time.Second
	fetchTimeout = 10 * time.Second
)

type tickMsg time.Time
type dataMsg struct {
	usage []k8s.Usage
	err   error
}

type model struct {
	title   string
	fetch   FetchFunc
	latest  []k8s.Usage
	err     error
	updated time.Time
	w, h    int
}

// Run launches the live dashboard and blocks until the user quits.
func Run(title string, fetch FetchFunc) error {
	m := model{title: title, fetch: fetch}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd { return m.fetchCmd() }

func tickCmd() tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// fetchCmd runs one fetch with a generous timeout that is independent of the
// refresh interval, so a slow remote cluster doesn't race the next tick.
func (m model) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		u, err := m.fetch(ctx)
		return dataMsg{usage: u, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tickMsg:
		return m, m.fetchCmd()
	case dataMsg:
		// Keep the last good sample on screen when a refresh fails, so a
		// transient error doesn't blank the whole dashboard; just note it.
		m.err = msg.err
		if msg.err == nil {
			m.latest = msg.usage
			m.updated = time.Now()
		}
		// Drive the next fetch off completion (not a fixed tick) so fetches
		// never overlap, however slow the cluster is.
		return m, tickCmd()
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	status := "connecting..."
	if !m.updated.IsZero() {
		status = "updated " + m.updated.Format("15:04:05")
	}
	b.WriteString(headerStyle.Render(" 🦉 strix top — "+m.title) + dimStyle.Render("   "+status+" · q to quit"))
	b.WriteString("\n\n")

	// A hard failure with nothing to show: report it and stop.
	if m.err != nil && len(m.latest) == 0 {
		b.WriteString(errStyle.Render(m.err.Error()))
		b.WriteString("\n")
		return b.String()
	}

	// Connected but no rows yet: metrics-server may not have a sample yet, or
	// the selector matched nothing.
	if len(m.latest) == 0 {
		if m.updated.IsZero() {
			b.WriteString(dimStyle.Render("connecting to the cluster…"))
		} else {
			b.WriteString(dimStyle.Render("no usage to show — waiting for metrics, or nothing matched the selector"))
		}
		b.WriteString("\n")
		return b.String()
	}

	rows := m.latest
	if max := m.maxRows(); max > 0 && len(rows) > max {
		rows = rows[:max]
	}
	for _, u := range rows {
		b.WriteString(m.renderRow(u))
	}

	// A refresh failed but we still have the previous sample: keep showing it
	// with a dim note rather than blanking the dashboard.
	if m.err != nil {
		b.WriteString(dimStyle.Render("last refresh failed: " + m.err.Error()))
		b.WriteString("\n")
	}
	return b.String()
}

// maxRows is how many 4-line entries (name, CPU, MEM, blank) fit in the window.
func (m model) maxRows() int {
	if m.h <= 0 {
		return 0
	}
	return (m.h - 3) / 4
}

func (m model) renderRow(u k8s.Usage) string {
	name := u.Name
	if u.Namespace != "" {
		name = u.Namespace + "/" + u.Name
	}

	cpuLine := fmt.Sprintf("  %s %9s  %s",
		labelStyle.Render("CPU"), fmtCPU(u.CPUUsed.MilliValue()), gauge(u.CPUPercent()))
	memLine := fmt.Sprintf("  %s %9s  %s",
		labelStyle.Render("MEM"), fmtMem(u.MemUsed.Value()), gauge(u.MemPercent()))

	return nameStyle.Render(name) + "\n" + cpuLine + "\n" + memLine + "\n\n"
}
