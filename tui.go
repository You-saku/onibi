package main

// tui.go is the interactive full-screen view, built on Bubble Tea (Charm's
// Elm-style TUI framework) with Lip Gloss for styling. All port logic lives in
// ports.go; this file is purely presentation + input handling.
//
//   Model  = the whole UI state (below)
//   Init   = start the auto-refresh ticker
//   Update = fold a message (key / tick / resize) into new state
//   View   = render the current state to a string
//
// Scope today: visualize the processes haunting your ports, and kill the
// selected one. New capabilities should slot in as new Update cases + View
// sections without disturbing this structure.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- styles ----
var (
	styTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	styDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	stySel    = lipgloss.NewStyle().Reverse(true)
	styLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styRed    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	styYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// ---- messages ----
type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---- model ----
type model struct {
	rows       []Listener
	sel        int
	filter     string
	filterMode bool
	auto       bool
	force      bool // SIGKILL when true, else SIGTERM
	dryRun     bool
	status     string
	confirm    *Listener // non-nil while the kill confirmation is showing
	width      int
	height     int
}

func runTUI(dryRun bool) {
	m := &model{auto: true, dryRun: dryRun, width: 100, height: 40}
	m.refresh()
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Println("onibi:", err)
	}
}

func (m *model) Init() tea.Cmd { return tick() }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if m.auto && !m.filterMode && m.confirm == nil {
			m.refresh()
		}
		return m, tick()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 1) Confirmation modal has priority.
	if m.confirm != nil {
		switch key {
		case "y", "Y":
			m.doKill(*m.confirm)
			m.confirm = nil
		case "n", "N", "esc":
			m.confirm = nil
		}
		return m, nil
	}

	// 2) Filter text entry.
	if m.filterMode {
		switch key {
		case "enter", "esc":
			m.filterMode = false
		case "backspace":
			if m.filter != "" {
				m.filter = m.filter[:len(m.filter)-1]
			}
		default:
			if len(msg.Runes) == 1 {
				m.filter += string(msg.Runes)
			}
		}
		m.sel = 0
		return m, nil
	}

	// 3) Normal navigation.
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.sel > 0 {
			m.sel--
		}
	case "down", "j":
		if m.sel < len(m.visible())-1 {
			m.sel++
		}
	case "/":
		m.filterMode = true
		m.filter = ""
	case "esc":
		m.filter = ""
	case "r":
		m.refresh()
	case "a":
		m.auto = !m.auto
	case "f":
		m.force = !m.force
	case "enter": // open the kill confirmation for the selected row
		if sel := m.selected(); sel != nil {
			l := *sel
			m.confirm = &l
		}
	}
	return m, nil
}

func (m *model) refresh() {
	m.rows = Snapshot()
	if n := len(m.visible()); m.sel >= n {
		m.sel = n - 1
	}
	if m.sel < 0 {
		m.sel = 0
	}
	suffix := ""
	if m.dryRun {
		suffix = " · dry-run"
	}
	m.status = fmt.Sprintf("%d haunting · %s%s", len(m.rows), time.Now().Format("15:04:05"), suffix)
}

func (m *model) visible() []Listener {
	if m.filter == "" {
		return m.rows
	}
	q := strings.ToLower(m.filter)
	var out []Listener
	for _, r := range m.rows {
		hay := strings.ToLower(strconv.Itoa(r.Port) + " " + strconv.Itoa(r.PID) + " " + r.Project + " " + r.Args)
		if strings.Contains(hay, q) {
			out = append(out, r)
		}
	}
	return out
}

func (m *model) doKill(l Listener) {
	r := killProcess(l.PID, m.force, m.dryRun)
	switch {
	case r.DryRun:
		m.status = fmt.Sprintf("dry-run: would kill :%d pid %d", l.Port, l.PID)
	case r.OK:
		m.status = fmt.Sprintf("killed :%d pid %d", l.Port, l.PID)
		waitGone(l.PID, 800*time.Millisecond)
	default:
		m.status = fmt.Sprintf("failed to kill pid %d: %s", l.PID, r.Reason)
	}
	m.refresh()
}

func (m *model) View() string {
	var b strings.Builder

	b.WriteString(styTitle.Render(" onibi") + "  " + styDim.Render(m.status) + "\n")
	help := fmt.Sprintf(" up/down move · enter kill · / filter · r refresh · a auto(%s) · f %s · q quit",
		onOff(m.auto), sigName(m.force))
	b.WriteString(styDim.Render(help) + "\n")

	if m.filterMode || m.filter != "" {
		caret := ""
		if m.filterMode {
			caret = "_"
		}
		b.WriteString(styYellow.Render(" filter: "+m.filter+caret) + "\n")
	}

	header := fmt.Sprintf(" %-6s %-8s %-11s %-18s %s", "PORT", "PID", "AGE", "PROJECT", "COMMAND")
	b.WriteString(styDim.Render(header) + "\n")

	list := m.visible()
	if len(list) == 0 {
		b.WriteString(styDim.Render(" no listening ports in sight") + "\n")
	}
	for i, r := range list {
		maxCmd := m.width - 48
		if maxCmd < 10 {
			maxCmd = 10
		}
		age := ageStyle(AgeSeconds(r.Age)).Render(fmt.Sprintf("%-11s", dash(r.Age)))
		line := fmt.Sprintf(" %-6d %-8d %s %-18s %s",
			r.Port, r.PID, age,
			truncate(dash(r.Project), 18),
			truncate(firstNonEmpty(r.Args, r.Command), maxCmd),
		)
		if i == m.sel {
			plain := fmt.Sprintf(" %-6d %-8d %-11s %-18s %s",
				r.Port, r.PID, dash(r.Age),
				truncate(dash(r.Project), 18),
				truncate(firstNonEmpty(r.Args, r.Command), maxCmd))
			b.WriteString(stySel.Render(plain) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}

	// Kill confirmation modal.
	if m.confirm != nil {
		label := firstNonEmpty(m.confirm.Project, m.confirm.Command)
		b.WriteString("\n" +
			styRed.Render(fmt.Sprintf(" Kill :%d pid %d (%s) with %s?", m.confirm.Port, m.confirm.PID, label, sigName(m.force))) +
			styDim.Render("  y / n") + "\n")
		return b.String()
	}

	// Detail panel for the selected process — the heart of a "visualize" tool.
	if sel := m.selected(); sel != nil {
		b.WriteString("\n")
		b.WriteString(detailLine("address", fmt.Sprintf("%s:%d", dash(sel.Address), sel.Port)))
		b.WriteString(detailLine("pid", strconv.Itoa(sel.PID)))
		b.WriteString(detailLine("uptime", dash(sel.Age)))
		b.WriteString(detailLine("project", dash(sel.Project)))
		b.WriteString(detailLine("cwd", dash(sel.Cwd)))
		b.WriteString(detailLine("command", dash(firstNonEmpty(sel.Args, sel.Command))))
	}

	return b.String()
}

func (m *model) selected() *Listener {
	v := m.visible()
	if m.sel >= 0 && m.sel < len(v) {
		return &v[m.sel]
	}
	return nil
}

func detailLine(label, value string) string {
	return styLabel.Render(fmt.Sprintf(" %8s ", label)) + value + "\n"
}

// ---- small view helpers ----

func ageStyle(sec int) lipgloss.Style {
	switch {
	case sec >= 8*3600:
		return styRed
	case sec >= 2*3600:
		return styYellow
	default:
		return styGreen
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func sigName(force bool) string {
	if force {
		return "SIGKILL"
	}
	return "SIGTERM"
}
