package prompt

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	unselectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	titleStyle      = lipgloss.NewStyle().Bold(true).MarginBottom(1)
)

// Option represents a selectable item.
type Option struct {
	Label string
	Value string
}

type selectModel struct {
	title    string
	options  []Option
	cursor   int
	chosen   string
	quitting bool
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.chosen = m.options[m.cursor].Value
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectModel) View() string {
	s := titleStyle.Render(m.title) + "\n"
	for i, opt := range m.options {
		if i == m.cursor {
			s += selectedStyle.Render(fmt.Sprintf("  > %s", opt.Label)) + "\n"
		} else {
			s += unselectedStyle.Render(fmt.Sprintf("    %s", opt.Label)) + "\n"
		}
	}
	s += unselectedStyle.Render("\n  ↑/↓ to move, enter to select")
	return s
}

// Select displays an interactive selection list and returns the chosen value.
func Select(title string, options []Option) (string, error) {
	m := selectModel{title: title, options: options}
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return "", err
	}
	final := result.(selectModel)
	if final.quitting && final.chosen == "" {
		return "", fmt.Errorf("selection cancelled")
	}
	return final.chosen, nil
}
