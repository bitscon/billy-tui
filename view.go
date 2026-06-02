package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// renderMarkdown renders a markdown string with a fresh Glamour renderer.
// Prefer renderResponse() on the model which uses the cached renderer.
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

// helpContent returns the keybinding reference shown in the help overlay.
var helpContent = strings.TrimSpace(`
  Keys
  ────────────────────────────────
  enter         Send message
  ↑ / ↓         Cycle input history
  tab           Toggle scroll / input focus
  page up/dn    Scroll chat
  ctrl+l        Clear chat
  ctrl+u        Clear input line
  ctrl+s        Save debug log
  :             Command palette
  ?             This help  (any key to close)
  ctrl+c        Quit

  Commands  (type : then command)
  ────────────────────────────────
  :clear        Clear chat
  :export       Export conversation to ~/billy-chat-DATE.md
  :session new  Start a new session
  :help         Show this screen
`)

func (m model) View() string {
	if !m.ready {
		return "Initialising…\n"
	}

	chatWidth := (m.width * 7) / 10

	// ── status bar ───────────────────────────────────────────────────────────
	var statusParts []string

	base := "Billy"
	if m.sidebar.model != "" {
		base += " │ " + m.sidebar.model
	}
	if m.sidebar.mode != "" {
		base += " │ " + m.sidebar.mode
	}
	statusParts = append(statusParts, StatusBarStyle.Render(base))

	// session (abbreviated)
	sess := m.client.sessionID
	if len(sess) > 16 {
		sess = sess[:16] + "…"
	}
	statusParts = append(statusParts, StatusBarStyle.Render("session:"+sess))

	// latency
	if m.lastLatency > 0 {
		statusParts = append(statusParts, StatusLatencyStyle.Render(fmt.Sprintf("%.1fs", m.lastLatency.Seconds())))
	}

	// live token counter during streaming
	if m.isStreaming && m.streamTokens > 0 {
		statusParts = append(statusParts, StatusTokenStyle.Render(fmt.Sprintf("~%d tok", m.streamTokens)))
	}

	// notification (save / error feedback)
	if m.saveStatus != "" {
		statusParts = append(statusParts, StatusBarStyle.Render(m.saveStatus))
	}

	statusBar := StatusBarStyle.Width(m.width).Render(strings.Join(statusParts, "  "))

	// ── chat pane ────────────────────────────────────────────────────────────
	chatStyle := NormalPaneStyle
	if m.focusedPane == paneChat {
		chatStyle = FocusedPaneStyle
	}
	chatPane := chatStyle.
		Width(chatWidth).
		Height(m.height - 5).
		Render(m.chatViewport.View())

	// ── sidebar pane ─────────────────────────────────────────────────────────
	sidebarPane := NormalPaneStyle.
		Width(m.sidebarWidth).
		Height(m.height - 5).
		Render(renderSidebar(m.sidebar, m.sidebarWidth-2, m.height-5))

	joined := lipgloss.JoinHorizontal(lipgloss.Top, chatPane, sidebarPane)

	// ── bottom: input row + hint/command bar ─────────────────────────────────
	var inputRow, hintRow string

	if m.commandMode {
		// command palette: show ": " prefix + command input
		inputRow = CommandBarStyle.Width(m.width).Render(
			": " + m.commandInput.View(),
		)
		hintRow = HintBarStyle.Width(m.width).Render(
			"enter execute  esc cancel  :clear  :export  :session new  :help",
		)
	} else {
		focusIndicator := "─"
		if m.focusedPane == paneChat {
			focusIndicator = DimStyle.Render("scroll") + " tab→input"
		}
		inputRow = fmt.Sprintf("> %s", m.input.View())
		hintRow = HintBarStyle.Width(m.width).Render(
			focusIndicator + "  tab scroll  pgup/dn  ctrl+l clear  ctrl+s save  ? help",
		)
	}

	rendered := statusBar + "\n" + joined + "\n" + inputRow + "\n" + hintRow

	// ── governance alert border ───────────────────────────────────────────────
	if m.governanceAlertTicks > 0 {
		rendered = GovernanceBorderStyle.Render(rendered)
	}

	// ── help overlay ─────────────────────────────────────────────────────────
	if m.showHelp {
		overlay := HelpOverlayStyle.Render(helpContent)
		rendered = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			overlay,
			lipgloss.WithWhitespaceBackground(lipgloss.AdaptiveColor{Dark: "#111111", Light: "#DDDDDD"}),
		)
	}

	return rendered
}

// DimStyle is a convenience alias exposed for update.go hint text.
var DimStyle = lipgloss.NewStyle().Foreground(colorSubtle)
