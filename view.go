package main

import (
	"fmt"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// renderMarkdown renders a markdown string using Glamour with word-wrap at the
// given width. Falls back to the plain string on any error.
func renderMarkdown(s string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return s
	}
	out, err := r.Render(s)
	if err != nil {
		return s
	}
	return out
}

func (m model) View() string {
	if !m.ready {
		return "Initialising...\n"
	}

	chatWidth := (m.width * 7) / 10

	// Top status bar
	statusText := "Billy — v0.1 │ localhost:5001"
	if m.saveStatus != "" {
		statusText = m.saveStatus
	} else if m.sidebar.runtimeStatus != "" {
		statusText = m.sidebar.runtimeStatus
	}
	statusBar := StatusBarStyle.Width(m.width).Render(statusText)

	// Chat pane — content managed exclusively by updateChatViewport() in Update()
	chatPane := lipgloss.NewStyle().
		Width(chatWidth).
		Height(m.height - 4).
		Border(lipgloss.RoundedBorder()).
		Render(m.chatViewport.View())

	// Sidebar pane
	sidebarPane := lipgloss.NewStyle().
		Width(m.sidebarWidth).
		Height(m.height - 4).
		Border(lipgloss.RoundedBorder()).
		Render(renderSidebar(m.sidebar, m.sidebarWidth-2, m.height-4))

	// Join chat and sidebar horizontally
	joined := lipgloss.JoinHorizontal(lipgloss.Top, chatPane, sidebarPane)

	// Bottom input bar
	inputBar := fmt.Sprintf("> %s", m.input.View())

	rendered := statusBar + "\n" + joined + "\n" + inputBar
	if m.governanceAlertTicks > 0 {
		rendered = GovernanceBorderStyle.Render(rendered)
	}
	return rendered
}
