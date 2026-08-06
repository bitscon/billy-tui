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
  ctrl+j        Newline (multi-line input)
  esc           Abandon the reply Billy is generating
  ↑ / ↓         Cycle input history
  tab           Toggle scroll / input focus
  page up/dn    Scroll chat
  ctrl+l        Clear chat
  ctrl+u        Clear input line
  ctrl+s        Save debug log
  ctrl+t        Toggle mouse capture (off = select/copy text)
  :             Command palette
  ?             This help  (any key to close)
  ctrl+c        Quit

  Commands  (type : then command)
  ────────────────────────────────
  :clear        Clear chat
  :export       Export conversation to ~/.billy/exports/
  :session new  Start a new session
  :model        Pick a model (interactive)
  :model P [M]  Switch to provider P (optional model M)
  :help         Show this screen
`)

func (m model) View() string {
	if !m.ready {
		return "Initialising…\n"
	}

	// Guard against terminals too small to lay out the two-pane UI.
	if m.width < 60 || m.height < 8 {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			DimStyle.Render("Terminal too narrow\nresize to at least 60×8"),
		)
	}

	chatWidth := (m.width * 7) / 10

	// The input box may occupy several rows (multi-line input); the panes take
	// whatever height is left after the status row, the input, the hint row, and
	// the pane border. refreshInputHeight keeps m.input.Height() and the viewport
	// height consistent with this same arithmetic.
	inputRows := m.input.Height()
	paneHeight := m.height - 4 - inputRows
	if paneHeight < 4 {
		paneHeight = 4
	}

	// ── status bar ───────────────────────────────────────────────────────────
	var statusParts []string

	base := "Billy"
	if m.sidebar.model != "" {
		base += " │ " + m.sidebar.model
	}
	if m.sidebar.provider != "" {
		base += " │ " + m.sidebar.provider
	}
	statusParts = append(statusParts, StatusBarStyle.Render(base))

	// session (abbreviated)
	sess := truncate(m.client.sessionID, 16)
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
		Height(paneHeight).
		Render(m.chatViewport.View())

	// ── sidebar pane ─────────────────────────────────────────────────────────
	sidebarPane := NormalPaneStyle.
		Width(m.sidebarWidth).
		Height(paneHeight).
		Render(renderSidebar(m.sidebar, m.sidebarWidth-2, paneHeight))

	joined := lipgloss.JoinHorizontal(lipgloss.Top, chatPane, sidebarPane)

	// ── bottom: input row + hint/command bar ─────────────────────────────────
	var inputRow, hintRow string

	if m.commandMode {
		// command palette: show ": " prefix + command input
		inputRow = CommandBarStyle.Width(m.width).Render(
			": " + m.commandInput.View(),
		)
		hintRow = HintBarStyle.Width(m.width).Render(
			"enter execute  esc cancel  :model  :clear  :export  :session new  :help",
		)
	} else {
		focusIndicator := "─"
		if m.focusedPane == paneChat {
			focusIndicator = DimStyle.Render("scroll") + " tab→input"
		}
		// The "> " prompt is rendered by the textarea itself (m.input.Prompt) so
		// it prefixes every line of a multi-line entry, not just the first.
		inputRow = m.input.View()
		hintRow = HintBarStyle.Width(m.width).Render(
			focusIndicator + "  ctrl+j newline  tab scroll  ctrl+l clear  ctrl+s save  ? help",
		)
	}

	rendered := statusBar + "\n" + joined + "\n" + inputRow + "\n" + hintRow

	// ── governance alert border ───────────────────────────────────────────────
	if m.governanceAlertTicks > 0 {
		rendered = GovernanceBorderStyle.Render(rendered)
	}

	// ── model picker overlay ──────────────────────────────────────────────────
	if m.modelPickerMode {
		start, end := modelPickerWindow(m.modelPickerIdx, len(m.modelOptions), m.height)

		var b strings.Builder
		b.WriteString(SectionHeaderStyle.Render("Select a model") + "\n")
		b.WriteString(DimStyle.Render("↑/↓ move · enter switch · esc cancel") + "\n\n")
		if start > 0 {
			b.WriteString(DimStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
		}
		for i := start; i < end; i++ {
			opt := m.modelOptions[i]
			if i == m.modelPickerIdx {
				b.WriteString(UserInputStyle.Render("▸ "+opt.label) + "\n")
			} else {
				b.WriteString("  " + opt.label + "\n")
			}
		}
		if end < len(m.modelOptions) {
			b.WriteString(DimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.modelOptions)-end)) + "\n")
		}
		overlay := HelpOverlayStyle.Render(strings.TrimRight(b.String(), "\n"))
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			overlay,
			lipgloss.WithWhitespaceBackground(lipgloss.AdaptiveColor{Dark: "#111111", Light: "#DDDDDD"}),
		)
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

// modelPickerWindow returns the [start,end) slice of model options to display so
// a long list (e.g. an Ollama host with many models) never overflows the screen.
// The window is sized to the terminal height, keeps the selected index visible
// (centered where possible), and is clamped to the option count. Callers show a
// "N more" affordance when start>0 or end<total.
func modelPickerWindow(idx, total, termHeight int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	// Chrome inside the overlay: header + hint + blank (3), the two "N more"
	// rows (2), and the overlay border (2). Reserve that and keep at least 3
	// option rows visible even on a short terminal.
	visible := termHeight - 10
	if visible < 3 {
		visible = 3
	}
	if visible >= total {
		return 0, total
	}
	start = idx - visible/2
	if start < 0 {
		start = 0
	}
	if start > total-visible {
		start = total - visible
	}
	return start, start + visible
}

// DimStyle is a convenience alias exposed for update.go hint text.
var DimStyle = lipgloss.NewStyle().Foreground(colorSubtle)
