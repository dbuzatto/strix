package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dbuzatto/strix/internal/k8s"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestViewRenders(t *testing.T) {
	var m tea.Model = model{title: "Nodes"}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	sample := []k8s.Usage{{
		Name:     "node-1",
		CPUUsed:  resource.MustParse("1500m"),
		CPUTotal: resource.MustParse("4"),
		MemUsed:  resource.MustParse("8Gi"),
		MemTotal: resource.MustParse("16Gi"),
	}}
	m, _ = m.Update(dataMsg{usage: sample})

	out := m.View()
	for _, want := range []string{"node-1", "CPU", "MEM", "50%"} { // mem 8Gi/16Gi = 50%
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q in:\n%s", want, out)
		}
	}
}

func TestQuitOnKey(t *testing.T) {
	var m tea.Model = model{}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected a quit command on 'q'")
	}
}
