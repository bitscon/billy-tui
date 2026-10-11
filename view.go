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
  y / n         Approve / decline when Billy asks (or type a reply)
  1…9 / enter   Choose when Billy offers you options (esc closes, sends nothing)
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
                When routing is auto, the picker sets the pin
  :routing      Show/toggle auto-routing (interactive)
  :routing MODE Set routing to auto or pinned
  :brains       Brain floors — view/set each role's minimum brain
  :permissions  What the cloud brain may read — view and change
  :help         Show this screen
`)

// statusBase builds the leading status-bar segment. Under auto routing the
// provider/model pair from the config is the pin/home target, not necessarily
// what answered — so the bar reports the routing state (plus the last answering
// model, when the runtime reported one) instead of presenting the pair as the
// answerer. Any other mode — "pinned", or "" from a legacy runtime — must stay
// byte-identical to the pre-conductor display (contract §6).
func statusBase(routingMode string, lastBrain *BrainReport, pinModel, pinProvider string) string {
	base := "Billy"
	if routingMode == "auto" {
		base += " │ routing auto"
		if lastBrain != nil {
			base += " │ last: " + lastBrain.ModelID
		}
		return base
	}
	if pinModel != "" {
		base += " │ " + pinModel
	}
	if pinProvider != "" {
		base += " │ " + pinProvider
	}
	return base
}

// paneLayoutFits reports whether View() is actually drawing the interface — the
// window size has arrived and the terminal clears the two-pane layout's floor.
// Below it only the resize notice is drawn, so an overlay must not own the
// keyboard there: a choice the operator cannot see is not a choice.
func (m model) paneLayoutFits() bool {
	return m.ready && m.width >= 60 && m.height >= 8
}

func (m model) View() string {
	if !m.ready {
		return "Initialising…\n"
	}

	// Guard against terminals too small to lay out the two-pane UI.
	if !m.paneLayoutFits() {
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

	statusParts = append(statusParts, StatusBarStyle.Render(
		statusBase(m.routingMode, m.lastBrain, m.sidebar.model, m.sidebar.provider)))

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
		Render(renderSidebar(m.sidebar, m.routingMode, m.lastBrain, m.sidebarWidth-2, paneHeight))

	joined := lipgloss.JoinHorizontal(lipgloss.Top, chatPane, sidebarPane)

	// ── bottom: input row + hint/command bar ─────────────────────────────────
	var inputRow, hintRow string

	if m.commandMode {
		// command palette: show ": " prefix + command input
		inputRow = CommandBarStyle.Width(m.width).Render(
			": " + m.commandInput.View(),
		)
		hintRow = HintBarStyle.Width(m.width).Render(
			"enter execute  esc cancel  :model  :routing  :brains  :permissions  :clear  :export  :session new  :help",
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
		b.WriteString(DimStyle.Render("↑/↓ move · enter switch · esc cancel") + "\n")
		// Under auto routing a pick only sets the pin/home config (contract §7) —
		// say so BEFORE the choice, not just in the after-the-fact status line.
		if m.routingMode == "auto" {
			b.WriteString(SidebarWarnStyle.Render("routing is auto — your pick sets the pin; the conductor may choose another brain per turn") + "\n")
		}
		b.WriteString("\n")
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

	// ── brain-floor screen overlay (Modernization 5) ─────────────────────────
	if m.floorMode {
		var b strings.Builder
		if m.floorTierPick && m.floors != nil && m.floorIdx < len(m.floorRoles) {
			// Inner stage: pick the floor tier for the selected role, from the
			// runtime's own ordered vocabulary — nothing hardcoded.
			role := m.floorRoles[m.floorIdx]
			current := m.floors.Roles[role]
			b.WriteString(SectionHeaderStyle.Render("Floor for "+role) + "\n")
			b.WriteString(DimStyle.Render("↑/↓ move · enter set · esc back") + "\n\n")
			for i, tier := range m.floors.Tiers {
				line := tier
				if tier == current {
					line += "  (current)"
				}
				if i == m.floorTierIdx {
					b.WriteString(UserInputStyle.Render("▸ "+line) + "\n")
				} else {
					b.WriteString("  " + line + "\n")
				}
			}
		} else {
			b.WriteString(SectionHeaderStyle.Render("Brain floors — the minimum brain each role may use") + "\n")
			b.WriteString(DimStyle.Render("↑/↓ move · enter change a floor · esc close") + "\n\n")
			switch {
			case m.floors == nil:
				// loading, unavailable, or errored — the notice below says which
			default:
				for i, role := range m.floorRoles {
					line := fmt.Sprintf("%-14s %s", truncate(role, 14), m.floors.Roles[role])
					if i == m.floorIdx {
						b.WriteString(UserInputStyle.Render("▸ "+line) + "\n")
					} else {
						b.WriteString("  " + line + "\n")
					}
				}
				if len(m.floorRoles) > 0 && m.floors.DefaultFloor != "" {
					b.WriteString(DimStyle.Render("any other role floors at "+m.floors.DefaultFloor) + "\n")
				}
			}
		}
		if m.floorNotice != "" {
			style := DimStyle
			if strings.HasPrefix(m.floorNotice, "⚠") {
				style = SidebarWarnStyle
			}
			b.WriteString("\n" + style.Render(m.floorNotice) + "\n")
		}
		overlay := HelpOverlayStyle.Render(strings.TrimRight(b.String(), "\n"))
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			overlay,
			lipgloss.WithWhitespaceBackground(lipgloss.AdaptiveColor{Dark: "#111111", Light: "#DDDDDD"}),
		)
	}

	// ── routing-mode picker overlay (Modernization 6) ────────────────────────
	if m.routingPickerMode {
		var b strings.Builder
		b.WriteString(SectionHeaderStyle.Render("Routing mode") + "\n")
		b.WriteString(DimStyle.Render("↑/↓ move · enter switch · esc cancel") + "\n\n")
		for i, mode := range routingModes {
			line := mode + " — " + routingModeBlurb(mode)
			if mode == m.routingMode {
				line += "  (current)"
			}
			if i == m.routingPickerIdx {
				b.WriteString(UserInputStyle.Render("▸ "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		overlay := HelpOverlayStyle.Render(strings.TrimRight(b.String(), "\n"))
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			overlay,
			lipgloss.WithWhitespaceBackground(lipgloss.AdaptiveColor{Dark: "#111111", Light: "#DDDDDD"}),
		)
	}

	// ── pending-question picker overlay (contract v3 §14/§15) ─────────────────
	// Offered ONLY when it can be drawn whole: below that the client degrades to
	// exactly today's behaviour — the reply text behind it carries the question
	// and its numbered list, and the input box answers it. questionPickerActive
	// is the one predicate, shared with the key handler, so the screen and the
	// keyboard can never disagree about whether a picker is on offer.
	if fits, questionRows, optionRows := m.questionOverlayPlan(); fits {
		q := m.pendingQuestion
		width := questionWrapWidth(m.width)
		wrap := lipgloss.NewStyle().Width(width)
		start, end := questionOptionWindow(m.questionPickerIdx, len(q.Options), optionRows)

		var b strings.Builder
		b.WriteString(SectionHeaderStyle.Render(questionPickerTitle) + "\n")
		b.WriteString(DimStyle.Render(truncateCells(
			questionPickerHint(q, start == 0 && end == len(q.Options)), width)) + "\n\n")
		// The question in the brain's OWN words, and the labels in the order and
		// under the numbers the runtime matches — never re-worded, re-ordered or
		// renumbered (contract v3 §15 rule 5). Control characters are made inert
		// for display only; a choice always sends the bytes the runtime sent.
		qLines := strings.Split(wrap.Render(sanitizeQuestionDisplay(q.Question)), "\n")
		b.WriteString(strings.Join(clampQuestionLines(qLines, questionRows, width), "\n") + "\n")
		if subject := questionSubjectLine(q); subject != "" {
			b.WriteString(DimStyle.Render(truncateCells(sanitizeQuestionDisplay(subject), width)) + "\n")
		}
		b.WriteString("\n")
		if start > 0 {
			b.WriteString(DimStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
		}
		for i := start; i < end; i++ {
			// width-2: every option row carries a two-cell prefix ("▸ " or two
			// spaces), which is outside the wrap width, so the line itself gets
			// two cells less or the box grows wider than the terminal.
			line := truncateCells(fmt.Sprintf("%d. %s", i+1, sanitizeQuestionDisplay(q.Options[i])), width-2)
			if i == m.questionPickerIdx {
				b.WriteString(UserInputStyle.Render("▸ "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		if end < len(q.Options) {
			b.WriteString(DimStyle.Render(fmt.Sprintf("  ↓ %d more", len(q.Options)-end)) + "\n")
		}
		if note := questionPickerNote(q); note != "" {
			b.WriteString("\n" + DimStyle.Render(wrap.Render(note)) + "\n")
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

// --- pending-question picker rendering (contract v3 §14/§15) ---

// questionPickerTitle is deliberately generic: the picker is the same screen for
// every kind of question, and the question's own words say what it is about.
const questionPickerTitle = "Billy needs an answer"

// questionMinOptionRows is the fewest option rows worth drawing. A list longer
// than this is windowed rather than refused, so a long list does not cost the
// operator the picker.
const questionMinOptionRows = 3

// questionWrapWidth bounds the wrap width of the question text inside the
// overlay so a long sentence does not run off a wide terminal or overflow a
// narrow one. The overlay's own padding then keeps the box inside the terminal.
func questionWrapWidth(termWidth int) int {
	w := termWidth - 8
	if w > 72 {
		w = 72
	}
	if w < 24 {
		w = 24
	}
	return w
}

// questionPickerHint is the key line under the title. The number keys are
// offered only when the WHOLE list is on screen: with a windowed list a number
// would name an option the operator cannot see.
func questionPickerHint(q *PendingQuestion, allVisible bool) string {
	keys := "↑/↓ move · enter choose"
	if n := questionNumberKeys(q); n > 1 && allVisible {
		keys = fmt.Sprintf("↑/↓ move · 1–%d choose · enter choose", n)
	}
	return keys + " · esc close (nothing sent)"
}

// questionSubjectLine names what a permission request is about, from the
// runtime's own structured fields — never parsed out of the sentence, and empty
// on every kind that sends neither.
func questionSubjectLine(q *PendingQuestion) string {
	switch {
	case q.Tool != "" && q.Target != "":
		return q.Tool + " · " + q.Target
	case q.Tool != "":
		return q.Tool
	default:
		return q.Target
	}
}

// questionPickerNote is the one line under the options that says how THIS
// question is answered, where that differs from the ordinary case. Empty
// otherwise — the picker adds no commentary of its own to a plain choice.
func questionPickerNote(q *PendingQuestion) string {
	switch {
	case questionTypedAnswerOnly(q.Kind):
		return "This one is answered by the words, not a number — choosing sends the option exactly as written."
	case q.AllowFreeText:
		return "Your own words work too — close this and type them."
	default:
		return ""
	}
}

// sanitizeQuestionDisplay makes a question or an option label safe to put on a
// terminal: every control character, and every bidirectional override, becomes a
// single space.
//
// A DISPLAY transform only — what a choice SENDS is always the bytes the runtime
// sent (answerForOption), so a fenced kind's answer still matches the runtime's
// own stored label exactly. Three self-review findings close here: a question
// carrying CSI sequences could repaint the screen the operator is consenting on
// (the runtime's egress guards neutralise tool markup and persona claims, not
// C0/C1 controls); a newline inside a label rendered a second, forged numbered
// line under a real one; and a bidi override could make a label read as
// something other than the bytes a choice sends. None of these is text the
// operator can read, so making them inert is not a re-wording of the brain's
// words (contract v3 §15 rule 5).
func sanitizeQuestionDisplay(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f, r >= 0x80 && r <= 0x9f:
			b.WriteRune(' ')
		case r == 0x200e || r == 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncateCells trims s to at most max DISPLAY CELLS, marking a cut with an
// ellipsis. Cells, not runes: a label of wide characters is twice as wide as its
// rune count, and bounding by runes let a CJK label render a box wider than the
// terminal (self-review wave 2).
func truncateCells(s string, max int) string {
	if max < 1 || lipgloss.Width(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		r = r[:max] // one cell per rune at minimum, so this can only help
	}
	for len(r) > 0 {
		candidate := string(r) + "…"
		if lipgloss.Width(candidate) <= max {
			return candidate
		}
		r = r[:len(r)-1]
	}
	return "…"
}

// questionOverlayChrome counts the rows the overlay spends on everything that is
// NOT the question text or the option list, for this question on this terminal:
// the style's border and padding, the title, the key line, the blank under it,
// the blank above the options, a subject line where there is one, and the note
// (a blank plus its wrapped lines) where there is one.
//
// Computed from what will actually be written, never from a constant: a constant
// under-counted the padding, the subject and the wrapped note by three to five
// rows, and the overflow came off the TOP of the screen — taking the title and
// the key line with it (self-review wave 2).
func (m model) questionOverlayChrome(q *PendingQuestion) int {
	rows := HelpOverlayStyle.GetVerticalFrameSize()
	rows += 1 // the title
	rows += 1 // the key line, truncated to one row on render
	rows += 2 // the blank under the key line, the blank above the options
	if questionSubjectLine(q) != "" {
		rows++
	}
	if note := questionPickerNote(q); note != "" {
		rendered := lipgloss.NewStyle().Width(questionWrapWidth(m.width)).Render(note)
		rows += 1 + strings.Count(rendered, "\n") + 1
	}
	return rows
}

// questionOverlayPlan decides whether the picker can be drawn whole on this
// terminal and, if so, how many rows the question text and the option list each
// get. fits=false means no overlay and no keys: a choice the operator cannot see
// is not a choice (contract v3 §15 rule 6), and the client degrades to the
// numbered list in the reply text.
func (m model) questionOverlayPlan() (fits bool, questionRows, optionRows int) {
	q := m.pendingQuestion
	if q == nil || !m.questionPickerMode || !m.paneLayoutFits() {
		return false, 0, 0
	}
	chrome := m.questionOverlayChrome(q)
	// A windowed list costs two "N more" rows at worst; reserved up front so the
	// budget holds whichever way the window lands.
	moreRows := 0
	if len(q.Options) > questionMinOptionRows {
		moreRows = 2
	}
	minOptions := len(q.Options)
	if minOptions > questionMinOptionRows {
		minOptions = questionMinOptionRows
	}
	// One row for the question, plus the fewest options worth showing.
	if m.height < chrome+1+minOptions+moreRows {
		return false, 0, 0
	}
	budget := m.height - chrome - moreRows
	optionRows = len(q.Options)
	if optionRows > budget-1 {
		optionRows = budget - 1
	}
	questionRows = budget - optionRows
	if questionRows < 1 {
		questionRows = 1
	}
	return true, questionRows, optionRows
}

// questionOptionWindow returns the [start,end) slice of options to render, sized
// to the rows available and keeping the selection visible (centred where it
// can be). Callers show a "N more" row when start>0 or end<total.
func questionOptionWindow(idx, total, visible int) (start, end int) {
	if total <= 0 || visible <= 0 {
		return 0, 0
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

// clampQuestionLines keeps at most max rendered lines of the question, marking a
// cut with an ellipsis. The marked line is re-bounded to width: lipgloss has
// already padded it to the full wrap width, so appending to it blind made the
// box two columns wider than the terminal (self-review wave 2).
func clampQuestionLines(lines []string, max, width int) []string {
	if max < 1 {
		max = 1
	}
	if len(lines) <= max {
		return lines
	}
	out := append([]string(nil), lines[:max]...)
	out[max-1] = truncateCells(strings.TrimRight(out[max-1], " ")+" …", width)
	return out
}
