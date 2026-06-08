// Package tui renders the live `strix top --watch` dashboard: an htop-style
// view that polls metrics and draws real-time sparklines in the alternate
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
	interval = 2 * time.Second
	histLen  = 48
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
	cpuHist map[string][]float64
	memHist map[string][]float64
	err     error
	updated time.Time
	w, h    int
}

// Run launches the live dashboard and blocks until the user quits.
func Run(title string, fetch FetchFunc) error {
	m := model{
		title:   title,
		fetch:   fetch,
		cpuHist: map[string][]float64{},
		memHist: map[string][]float64{},
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd { return tea.Batch(m.fetchCmd(), tickCmd()) }

func tickCmd() tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), interval)
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
		return m, tea.Batch(m.fetchCmd(), tickCmd())
	case dataMsg:
		m.err = msg.err
		if msg.err == nil {
			m.latest = msg.usage
			m.updated = time.Now()
			m.record()
		}
	}
	return m, nil
}

// record appends the latest sample to each row's history and drops rows that
// have disappeared, so the history maps don't grow unbounded.
func (m *model) record() {
	seen := make(map[string]bool, len(m.latest))
	for _, u := range m.latest {
		k := key(u)
		seen[k] = true
		// Store percentages when a limit is known (so the sparkline shares the
		// 0..100 scale of the gauge); otherwise store the raw value and let the
		// sparkline auto-scale to its own window.
		m.cpuHist[k] = push(m.cpuHist[k], histVal(u.CPUPercent(), float64(u.CPUUsed.MilliValue())))
		m.memHist[k] = push(m.memHist[k], histVal(u.MemPercent(), float64(u.MemUsed.Value())))
	}
	for k := range m.cpuHist {
		if !seen[k] {
			delete(m.cpuHist, k)
			delete(m.memHist, k)
		}
	}
}

func (m model) View() string {
	var b strings.Builder

	status := "connecting..."
	if !m.updated.IsZero() {
		status = "updated " + m.updated.Format("15:04:05")
	}
	b.WriteString(headerStyle.Render(" 🦉 strix top — "+m.title) + dimStyle.Render("   "+status+" · q to quit"))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errStyle.Render(m.err.Error()))
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
	k := key(u)
	name := u.Name
	if u.Namespace != "" {
		name = u.Namespace + "/" + u.Name
	}

	cpuLine := fmt.Sprintf("  %s %9s  %s  %s",
		labelStyle.Render("CPU"), fmtCPU(u.CPUUsed.MilliValue()),
		gauge(u.CPUPercent()), spark(m.cpuHist[k], u.CPUPercent()))
	memLine := fmt.Sprintf("  %s %9s  %s  %s",
		labelStyle.Render("MEM"), fmtMem(u.MemUsed.Value()),
		gauge(u.MemPercent()), spark(m.memHist[k], u.MemPercent()))

	return nameStyle.Render(name) + "\n" + cpuLine + "\n" + memLine + "\n\n"
}

func key(u k8s.Usage) string { return u.Namespace + "/" + u.Name }

// histVal records the percentage when known, else the raw value.
func histVal(pct, raw float64) float64 {
	if pct >= 0 {
		return pct
	}
	return raw
}

func push(s []float64, v float64) []float64 {
	s = append(s, v)
	if len(s) > histLen {
		s = s[len(s)-histLen:]
	}
	return s
}
