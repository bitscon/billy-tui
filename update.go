package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type sidebarTickMsg struct{}

func waitForStream(events <-chan streamEvent, prompt string) tea.Cmd {
	return func() tea.Msg {
		return nextStreamMessage(events, prompt)
	}
}

func (m *model) appendStreamChunk(chunk string) {
	m.streamBuffer += chunk
	m.streamTokens += len(chunk)/4 + 1
}

// highlightStreamBuffer applies code-fence background styling during live stream.
// Completed responses are fully rendered by Glamour; this is the lightweight
// streaming preview (Phase 3b).
func highlightStreamBuffer(buf string) string {
	parts := strings.Split(buf, "```")
	var sb strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			sb.WriteString(CodeBlockStyle.Render(part))
		} else {
			sb.WriteString(part)
		}
	}
	return sb.String()
}

func (m *model) updateChatViewport() {
	if !m.ready {
		return
	}
	content := strings.Join(m.displayMessages, "\n")
	if m.liveMsg != "" {
		content += "\n" + m.liveMsg
	}
	m.chatViewport.SetContent(content)
	m.chatViewport.GotoBottom()
}

// renderResponse renders a completed Billy response using the cached Glamour
// renderer when available, falling back to a fresh renderer otherwise.
func (m *model) renderResponse(text string) string {
	var rendered string
	if m.mdRenderer != nil {
		if out, err := m.mdRenderer.Render(text); err == nil {
			rendered = out
		}
	}
	if rendered == "" {
		rendered = renderMarkdown(text, m.chatViewport.Width)
	}
	return strings.TrimLeft(rendered, "\n")
}

// execCommand handles the command palette. cmd arrives without the leading ":".
func (m *model) execCommand(raw string) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "clear", "c":
		m.messages = []string{}
		m.displayMessages = []string{}
		m.liveMsg = ""
		m.streamBuffer = ""
		m.updateChatViewport()
	case "export", "e":
		path, err := exportChat(m.messages, m.client.sessionID)
		if err != nil {
			m.saveStatus = "⚠️  Export failed: " + err.Error()
		} else {
			m.saveStatus = "✓ Exported → " + path
		}
		m.saveStatusTicks = 4
	case "session":
		if len(parts) > 1 && parts[1] == "new" {
			m.client.sessionID = fmt.Sprintf("tui-%d", time.Now().UnixNano())
			short := m.client.sessionID
			if len(short) > 20 {
				short = short[:20]
			}
			m.saveStatus = "✓ New session: " + short
			m.saveStatusTicks = 4
		}
	case "help", "h":
		m.showHelp = true
	}
}

func (m model) Init() tea.Cmd {
	healthCmd := func() tea.Msg {
		err := m.client.Health()
		return healthResultMsg{err: err}
	}
	sidebarTick := tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return sidebarTickMsg{} })
	return tea.Batch(textarea.Blink, m.spinner.Tick, healthCmd, sidebarTick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── window resize ────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		chatWidth := (msg.Width * 7) / 10
		m.sidebarWidth = msg.Width - chatWidth

		// Reserve 2 lines below panes: 1 for textarea row, 1 for hint/command bar.
		// Panes: Height() sets inner height; border adds 2 more → paneOuter = inner+2.
		// Layout: 1(status) + (inner+2)(panes) + 1(input) + 1(hint) = inner+5 = height
		// → inner = height-5
		vpHeight := msg.Height - 5
		if vpHeight < 4 {
			vpHeight = 4
		}
		if !m.ready {
			m.chatViewport = viewport.New(chatWidth, vpHeight)
			m.chatViewport.SetContent(strings.Join(m.displayMessages, "\n"))
			m.chatViewport.GotoBottom()
			m.ready = true
		} else {
			m.chatViewport.Width = chatWidth
			m.chatViewport.Height = vpHeight
		}
		m.input.SetWidth(msg.Width - 4)
		m.mdRenderer = newMdRenderer(chatWidth - 4)
		return m, nil

	// ── spinner ──────────────────────────────────────────────────────────────
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.thinking && !m.isStreaming {
			m.liveMsg = m.spinner.View() + " Billy is thinking…"
			m.updateChatViewport()
		}
		return m, cmd

	// ── health check ─────────────────────────────────────────────────────────
	case healthResultMsg:
		if msg.err != nil {
			plain := "⚠️  Cannot reach Billy at " + m.client.baseURL + ". Is billy-runtime running?"
			m.messages = append(m.messages, plain)
			m.displayMessages = append(m.displayMessages, ErrorStyle.Render(plain))
			m.updateChatViewport()
		}
		return m, nil

	// ── non-streaming response ────────────────────────────────────────────────
	case responseMsg:
		rendered := m.renderResponse(msg.text)
		m.messages = append(m.messages, "[Billy] "+msg.text)
		m.displayMessages = append(m.displayMessages, BillyResponseStyle.Render("[Billy] ")+rendered)
		m.thinking = false
		m.liveMsg = ""
		m.lastLatency = time.Since(m.requestStarted)
		m.updateChatViewport()
		return m, nil

	case errMsg:
		m.messages = append(m.messages, msg.text)
		m.displayMessages = append(m.displayMessages, ErrorStyle.Render(msg.text))
		m.thinking = false
		m.isStreaming = false
		m.liveMsg = ""
		m.updateChatViewport()
		return m, nil

	// ── streaming ────────────────────────────────────────────────────────────
	case StreamChunkMsg:
		if msg.Chunk != "" {
			m.appendStreamChunk(msg.Chunk)
			m.liveMsg = BillyResponseStyle.Render("[Billy] ") +
				highlightStreamBuffer(m.streamBuffer) + " █"
		}
		m.isStreaming = true
		m.thinking = false
		m.updateChatViewport()
		return m, waitForStream(msg.events, msg.Prompt)

	case StreamDoneMsg:
		if msg.FullText != "" {
			rendered := m.renderResponse(msg.FullText)
			m.messages = append(m.messages, "[Billy] "+msg.FullText)
			m.displayMessages = append(m.displayMessages, BillyResponseStyle.Render("[Billy] ")+rendered)
		}
		m.liveMsg = ""
		m.streamBuffer = ""
		m.isStreaming = false
		m.thinking = false
		m.lastLatency = time.Since(m.requestStarted)
		m.updateChatViewport()
		return m, nil

	case StreamErrMsg:
		m.streamBuffer = ""
		m.isStreaming = false
		m.liveMsg = ""
		m.thinking = true
		if msg.Prompt == "" {
			m.thinking = false
			m.messages = append(m.messages, "⚠️  streaming error")
			m.displayMessages = append(m.displayMessages, ErrorStyle.Render("⚠️  streaming error"))
			m.updateChatViewport()
			return m, nil
		}
		return m, ask(msg.Prompt, m.client.sessionID, m.client.baseURL)

	// ── sidebar polling ──────────────────────────────────────────────────────
	case sidebarTickMsg:
		if status, err := m.client.RuntimeStatus(); err == nil {
			parts := []string{}
			for _, k := range []string{"model", "mode", "version"} {
				if v, ok := status[k]; ok && v != "" {
					parts = append(parts, v)
				}
			}
			if len(parts) > 0 {
				m.sidebar.runtimeStatus = strings.Join(parts, " │ ")
			}
			m.sidebar.model = status["model"]
			m.sidebar.mode = status["mode"]
		}
		if events, err := m.client.TelemetryEvents(); err == nil {
			types := []string{}
			for _, e := range events {
				if t, ok := e["event_type"].(string); ok {
					types = append(types, t)
				}
			}
			if len(types) > 5 {
				types = types[len(types)-5:]
			}
			m.sidebar.recentEvents = types
			for _, t := range types {
				if t == "tool.call.denied" && t != m.lastGovernanceEvent {
					m.lastGovernanceEvent = t
					m.governanceAlertTicks = 3
					m.sidebar.governanceState = "🚫 action blocked"
					plain := "[Billy] 🛡️ Action blocked by governance policy."
					m.messages = append(m.messages, plain)
					m.displayMessages = append(m.displayMessages, ErrorStyle.Render(plain))
					m.updateChatViewport()
				}
			}
		}
		if m.governanceAlertTicks > 0 {
			m.governanceAlertTicks--
			if m.governanceAlertTicks == 0 {
				m.sidebar.governanceState = ""
			}
		}
		if m.saveStatusTicks > 0 {
			m.saveStatusTicks--
			if m.saveStatusTicks == 0 {
				m.saveStatus = ""
			}
		}
		return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return sidebarTickMsg{} })

	// ── keyboard ─────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		// ── help overlay: any key closes ──
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// ── command palette ──
		if m.commandMode {
			switch msg.String() {
			case "enter":
				m.execCommand(m.commandInput.Value())
				m.commandMode = false
				m.commandInput.Reset()
			case "esc", "ctrl+c":
				m.commandMode = false
				m.commandInput.Reset()
			default:
				var cmd tea.Cmd
				m.commandInput, cmd = m.commandInput.Update(msg)
				return m, cmd
			}
			return m, nil
		}

		// ── global shortcuts (work in any pane) ──
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+s":
			path, err := saveChat(m.messages, m.client.sessionID)
			if err != nil {
				m.saveStatus = "⚠️  Save failed: " + err.Error()
			} else {
				m.saveStatus = "✓ Saved → " + path
			}
			m.saveStatusTicks = 4
			return m, nil

		case "ctrl+l":
			m.messages = []string{}
			m.displayMessages = []string{}
			m.liveMsg = ""
			m.streamBuffer = ""
			m.updateChatViewport()
			return m, nil

		case "tab":
			if m.focusedPane == paneInput {
				m.focusedPane = paneChat
				m.input.Blur()
			} else {
				m.focusedPane = paneInput
				m.input.Focus()
			}
			return m, nil

		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.chatViewport, cmd = m.chatViewport.Update(msg)
			return m, cmd

		case "?":
			if m.input.Value() == "" {
				m.showHelp = !m.showHelp
				return m, nil
			}
		}

		// ── input-pane keys ──
		if m.focusedPane == paneInput {
			switch msg.String() {
			case "enter":
				if m.isStreaming || m.thinking {
					m.saveStatus = "Wait for current response…"
					m.saveStatusTicks = 2
					return m, nil
				}
				if m.input.Value() == "" {
					return m, nil
				}
				userMsg := m.input.Value()
				// push to history ring buffer
				m.inputHistory = append(m.inputHistory, userMsg)
				if len(m.inputHistory) > 50 {
					m.inputHistory = m.inputHistory[1:]
				}
				m.historyIdx = -1
				m.draftInput = ""

				m.messages = append(m.messages, "[You] "+userMsg)
				m.displayMessages = append(m.displayMessages, UserInputStyle.Render("[You] "+userMsg))
				m.input.Reset()
				m.thinking = true
				m.isStreaming = true
				m.streamBuffer = ""
				m.streamTokens = 0
				m.requestStarted = time.Now()
				m.liveMsg = BillyResponseStyle.Render("[Billy] ") + " █"
				m.updateChatViewport()
				return m, askStream(userMsg, m.client.sessionID, m.client.baseURL)

			case "up":
				if len(m.inputHistory) == 0 {
					return m, nil
				}
				if m.historyIdx == -1 {
					m.draftInput = m.input.Value()
				}
				if m.historyIdx < len(m.inputHistory)-1 {
					m.historyIdx++
					m.input.SetValue(m.inputHistory[len(m.inputHistory)-1-m.historyIdx])
				}
				return m, nil

			case "down":
				if m.historyIdx > 0 {
					m.historyIdx--
					m.input.SetValue(m.inputHistory[len(m.inputHistory)-1-m.historyIdx])
				} else if m.historyIdx == 0 {
					m.historyIdx = -1
					m.input.SetValue(m.draftInput)
				}
				return m, nil

			case "ctrl+u":
				m.input.Reset()
				m.historyIdx = -1
				m.draftInput = ""
				return m, nil

			case ":":
				if m.input.Value() == "" {
					m.commandMode = true
					m.commandInput.Focus()
					return m, nil
				}
			}
		}

		// ── chat-pane keys: scroll and escape back to input ──
		if m.focusedPane == paneChat {
			switch msg.String() {
			case "up", "down", "left", "right":
				var cmd tea.Cmd
				m.chatViewport, cmd = m.chatViewport.Update(msg)
				return m, cmd
			case "i", "enter", "esc":
				m.focusedPane = paneInput
				m.input.Focus()
				return m, nil
			}
		}
	}

	// ── pass events to focused component ────────────────────────────────────
	if m.focusedPane == paneInput {
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		cmds = append(cmds, inputCmd)
	}

	var vpCmd tea.Cmd
	m.chatViewport, vpCmd = m.chatViewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}
