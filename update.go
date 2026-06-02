package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
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
}

func (m model) Init() tea.Cmd {
	healthCmd := func() tea.Msg {
		err := m.client.Health()
		return healthResultMsg{err: err}
	}
	sidebarTick := tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return sidebarTickMsg{} })
	return tea.Batch(textinput.Blink, m.spinner.Tick, healthCmd, sidebarTick)
}

func (m *model) updateChatViewport() {
	if m.ready {
		content := strings.Join(m.displayMessages, "\n")
		if m.liveMsg != "" {
			content += "\n" + m.liveMsg
		}
		m.chatViewport.SetContent(content)
		m.chatViewport.GotoBottom()
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		chatWidth := (msg.Width * 7) / 10
		m.sidebarWidth = msg.Width - chatWidth

		if !m.ready {
			m.chatViewport = viewport.New(chatWidth, msg.Height-4)
			m.chatViewport.SetContent(strings.Join(m.displayMessages, "\n"))
			m.chatViewport.GotoBottom()
			m.ready = true
		} else {
			m.chatViewport.Width = chatWidth
			m.chatViewport.Height = msg.Height - 4
		}
		m.input.Width = msg.Width - 4

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.thinking && !m.isStreaming {
			m.liveMsg = m.spinner.View() + " Billy is thinking…"
			m.updateChatViewport()
		}
		return m, cmd

	case healthResultMsg:
		if msg.err != nil {
			plain := "⚠️  Cannot reach Billy at " + m.client.baseURL + ". Is billy-runtime running?"
			m.messages = append(m.messages, plain)
			m.displayMessages = append(m.displayMessages, ErrorStyle.Render(plain))
			m.updateChatViewport()
		}
		return m, nil

	case responseMsg:
		rendered := renderMarkdown(msg.text, m.chatViewport.Width)
		rendered = strings.TrimLeft(rendered, "\n")
		m.messages = append(m.messages, "[Billy] "+msg.text)
		m.displayMessages = append(m.displayMessages, BillyResponseStyle.Render("[Billy] ")+rendered)
		m.thinking = false
		m.liveMsg = ""
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

	case StreamChunkMsg:
		if msg.Chunk != "" {
			m.appendStreamChunk(msg.Chunk)
			m.liveMsg = BillyResponseStyle.Render("[Billy] ") + m.streamBuffer + " █"
		}
		m.isStreaming = true
		m.thinking = false
		m.updateChatViewport()
		return m, waitForStream(msg.events, msg.Prompt)

	case StreamDoneMsg:
		if msg.FullText != "" {
			rendered := renderMarkdown(msg.FullText, m.chatViewport.Width)
			rendered = strings.TrimLeft(rendered, "\n")
			m.messages = append(m.messages, "[Billy] "+msg.FullText)
			m.displayMessages = append(m.displayMessages, BillyResponseStyle.Render("[Billy] ")+rendered)
		}
		m.liveMsg = ""
		m.streamBuffer = ""
		m.isStreaming = false
		m.thinking = false
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

	case sidebarTickMsg:
		// poll runtime status
		if status, err := m.client.RuntimeStatus(); err == nil {
			parts := []string{}
			if v, ok := status["model"]; ok && v != "" {
				parts = append(parts, v)
			}
			if v, ok := status["mode"]; ok && v != "" {
				parts = append(parts, v)
			}
			if v, ok := status["version"]; ok && v != "" {
				parts = append(parts, v)
			}
			if len(parts) > 0 {
				m.sidebar.runtimeStatus = strings.Join(parts, " │ ")
			}
		}
		// poll telemetry events
		if events, err := m.client.TelemetryEvents(); err == nil {
			types := []string{}
			for _, e := range events {
				if t, ok := e["event_type"].(string); ok {
					types = append(types, t)
				}
			}
			// take last 5
			if len(types) > 5 {
				types = types[len(types)-5:]
			}
			m.sidebar.recentEvents = types
			// check for governance block
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
		// decrement governance alert countdown
		if m.governanceAlertTicks > 0 {
			m.governanceAlertTicks--
			if m.governanceAlertTicks == 0 {
				m.sidebar.governanceState = "auto-approve"
			}
		}
		// decrement save status countdown
		if m.saveStatusTicks > 0 {
			m.saveStatusTicks--
			if m.saveStatusTicks == 0 {
				m.saveStatus = ""
			}
		}
		// schedule next tick
		return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return sidebarTickMsg{} })

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.isStreaming || m.thinking {
				m.saveStatus = "Wait for current response before sending another message"
				m.saveStatusTicks = 2
				return m, nil
			}
			if m.input.Value() == "" {
				return m, nil
			}
			userMsg := m.input.Value()
			m.messages = append(m.messages, "[You] "+userMsg)
			m.displayMessages = append(m.displayMessages, UserInputStyle.Render("[You] "+userMsg))
			m.input.Reset()
			m.thinking = true
			m.isStreaming = true
			m.streamBuffer = ""
			m.liveMsg = BillyResponseStyle.Render("[Billy] ") + " █"
			m.updateChatViewport()
			return m, askStream(userMsg, m.client.sessionID, m.client.baseURL)
		case "ctrl+s":
			path, err := saveChat(m.messages, m.client.sessionID)
			if err != nil {
				m.saveStatus = "⚠️  Save failed: " + err.Error()
			} else {
				m.saveStatus = "✓ Saved → " + path
			}
			m.saveStatusTicks = 4
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	var vpCmd tea.Cmd
	m.chatViewport, vpCmd = m.chatViewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}
