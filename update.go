package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type sidebarTickMsg struct{}

func waitForStream(events <-chan streamEvent, prompt string, gen int) tea.Cmd {
	return func() tea.Msg {
		return nextStreamMessage(events, prompt, gen)
	}
}

// releaseStream cancels and clears the in-flight turn's context, freeing its
// resources. Safe to call when no turn is active.
func (m *model) releaseStream() {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
}

func (m *model) appendStreamChunk(chunk string) {
	m.streamBuffer += chunk
	// Estimate from the whole buffer, not a running per-chunk sum. The old
	// `+= len(chunk)/4 + 1` added 1 per chunk (inflating the live count) and,
	// via per-chunk integer division, diverged from the non-streaming
	// len(text)/4. Deriving from the full buffer keeps both paths on one basis.
	m.streamTokens = len(m.streamBuffer) / 4
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

// rebuildDisplayMessages re-derives the styled transcript from the raw messages
// using the current renderer width. Called on terminal resize so Billy's
// markdown re-wraps to the new width instead of keeping stale line breaks. The
// styling rules mirror how each line is rendered when first appended.
func (m *model) rebuildDisplayMessages() {
	if len(m.messages) == 0 {
		return // keep the startup dim hint
	}
	rebuilt := make([]string, 0, len(m.messages))
	for _, raw := range m.messages {
		switch {
		case strings.HasPrefix(raw, "[You] "):
			rebuilt = append(rebuilt, UserInputStyle.Render(raw))
		case strings.HasPrefix(raw, "[Billy] "):
			rebuilt = append(rebuilt, BillyResponseStyle.Render("[Billy] ")+m.renderResponse(strings.TrimPrefix(raw, "[Billy] ")))
		default:
			// TUI notices (governance shield, ⚠️ health errors) and anything not
			// explicitly a [You]/[Billy] line render error-styled, never attributed.
			rebuilt = append(rebuilt, ErrorStyle.Render(raw))
		}
	}
	m.displayMessages = rebuilt
}

// restoreFailedPrompt puts the last submitted prompt back into the input after a
// turn fails, so the operator can resend without retyping. It does not clobber
// anything the operator has since typed.
func (m *model) restoreFailedPrompt() {
	if m.lastPrompt != "" && m.input.Value() == "" {
		m.input.SetValue(m.lastPrompt)
	}
}

// loadModels fetches available provider/models from the runtime and opens the picker.
func loadModels(c *billyClient) tea.Cmd {
	return func() tea.Msg {
		providers, err := c.ListModels()
		if err != nil {
			return modelsLoadedMsg{err: err}
		}
		var opts []modelOption
		for _, p := range providers {
			if p.DefaultModel != "" {
				opts = append(opts, modelOption{
					provider: p.ID, model: p.DefaultModel,
					label: p.ID + " · " + p.DefaultModel + "  (default)",
				})
			}
			for _, mdl := range p.Models {
				if mdl == p.DefaultModel {
					continue
				}
				opts = append(opts, modelOption{provider: p.ID, model: mdl, label: p.ID + " · " + mdl})
			}
		}
		return modelsLoadedMsg{options: opts}
	}
}

// setModel switches the active model via the sanctioned operator API. When
// switching within the same provider it preserves the operator's current
// base_url, so a custom host (e.g. a remote ollama) is not reset to the
// provider default.
func setModel(c *billyClient, provider, model string) tea.Cmd {
	return func() tea.Msg {
		baseURL := ""
		if cur, err := c.LLMConfig(); err == nil && cur.Provider == provider {
			baseURL = cur.BaseURL
		}
		cfg, err := c.SetLLMConfig(provider, model, baseURL)
		if err != nil {
			return modelSetMsg{provider: provider, model: model, err: err}
		}
		return modelSetMsg{provider: cfg.Provider, model: cfg.Model}
	}
}

// execCommand handles the command palette. cmd arrives without the leading ":".
func (m *model) execCommand(raw string) tea.Cmd {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return nil
	}
	switch parts[0] {
	case "model", "m":
		if len(parts) == 1 {
			return loadModels(m.client) // no arg → open picker
		}
		// :model <provider> [model]
		provider := parts[1]
		model := ""
		if len(parts) > 2 {
			model = strings.Join(parts[2:], " ")
		}
		m.saveStatus = "Switching…"
		m.saveStatusTicks = 4
		return setModel(m.client, provider, model)
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
	return nil
}

// handleTurnInProgress resolves an HTTP 409 (session_turn_in_progress) from the
// runtime. Billy is still finishing the previous turn, so the just-attempted
// request is dropped rather than retried (a retry would 409 again). The in-flight
// UI state is cleared, the just-typed prompt is restored to the input so the
// operator can resend once Billy responds, and a transient, non-alarming status
// line is shown.
func (m *model) handleTurnInProgress() (tea.Model, tea.Cmd) {
	m.releaseStream()
	m.thinking = false
	m.isStreaming = false
	m.liveMsg = ""
	m.streamBuffer = ""
	m.restoreFailedPrompt()
	m.saveStatus = turnInProgressMessage
	m.saveStatusTicks = 4
	m.updateChatViewport()
	return m, nil
}

func (m model) Init() tea.Cmd {
	healthCmd := func() tea.Msg {
		err := m.client.Health()
		return healthResultMsg{err: err}
	}
	// Kick off sidebar polling with a single immediate one-shot so the sidebar
	// is populated at startup instead of blank until the first tick. The
	// sidebarTickMsg handler self-reschedules every 5s, so this is the only poll
	// source — adding a second recurring tea.Tick here ran two self-rescheduling
	// chains in parallel and doubled the effective poll rate (~2.5s, 4 GETs each).
	sidebarNow := func() tea.Msg { return sidebarTickMsg{} }
	return tea.Batch(textarea.Blink, m.spinner.Tick, healthCmd, sidebarNow)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── window resize ────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		chatWidth := (msg.Width * 7) / 10
		if chatWidth < 24 {
			chatWidth = 24 // floor so the renderer/viewport never degenerate
		}
		m.sidebarWidth = msg.Width - chatWidth
		if m.sidebarWidth < 0 {
			m.sidebarWidth = 0
		}

		// Reserve 2 lines below panes: 1 for textarea row, 1 for hint/command bar.
		// Panes: Height() sets inner height; border adds 2 more → paneOuter = inner+2.
		// Layout: 1(status) + (inner+2)(panes) + 1(input) + 1(hint) = inner+5 = height
		// → inner = height-5
		vpHeight := msg.Height - 5
		if vpHeight < 4 {
			vpHeight = 4
		}
		mdWidth := chatWidth - 4
		if mdWidth < 20 {
			mdWidth = 20
		}
		m.mdRenderer = newMdRenderer(mdWidth)
		if !m.ready {
			m.chatViewport = viewport.New(chatWidth, vpHeight)
			m.chatViewport.SetContent(strings.Join(m.displayMessages, "\n"))
			m.chatViewport.GotoBottom()
			m.ready = true
		} else {
			m.chatViewport.Width = chatWidth
			m.chatViewport.Height = vpHeight
			// Re-flow existing messages to the new width (mdRenderer is already
			// updated above) so long replies don't keep stale line breaks.
			m.rebuildDisplayMessages()
			m.updateChatViewport()
		}
		m.input.SetWidth(msg.Width - 4)
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
		m.sidebar.connected = msg.err == nil
		if msg.err != nil {
			plain := "⚠️  Cannot reach Billy at " + m.client.baseURL + ". Is billy-runtime running?"
			m.messages = append(m.messages, plain)
			m.displayMessages = append(m.displayMessages, ErrorStyle.Render(plain))
			m.updateChatViewport()
		}
		return m, nil

	// ── model picker: list loaded ─────────────────────────────────────────────
	case modelsLoadedMsg:
		if msg.err != nil {
			m.saveStatus = "⚠️  models: " + msg.err.Error()
			m.saveStatusTicks = 4
			return m, nil
		}
		if len(msg.options) == 0 {
			m.saveStatus = "No models offered by runtime"
			m.saveStatusTicks = 4
			return m, nil
		}
		m.modelOptions = msg.options
		m.modelPickerIdx = 0
		m.modelPickerMode = true
		return m, nil

	// ── model switch result ───────────────────────────────────────────────────
	case modelSetMsg:
		if msg.err != nil {
			m.saveStatus = "⚠️  switch failed: " + msg.err.Error()
		} else {
			m.saveStatus = "✓ Switched to " + msg.model + " (" + msg.provider + ")"
			m.sidebar.model = msg.model
			m.sidebar.provider = msg.provider
		}
		m.saveStatusTicks = 4
		return m, nil

	// ── non-streaming response ────────────────────────────────────────────────
	case responseMsg:
		if msg.Gen != m.streamGen {
			return m, nil // stale: a newer turn started or this one was abandoned
		}
		m.releaseStream()
		rendered := m.renderResponse(msg.text)
		m.messages = append(m.messages, "[Billy] "+msg.text)
		m.displayMessages = append(m.displayMessages, BillyResponseStyle.Render("[Billy] ")+rendered)
		m.thinking = false
		m.liveMsg = ""
		m.lastLatency = time.Since(m.requestStarted)
		m.sidebar.lastLatency = fmt.Sprintf("%.1fs", m.lastLatency.Seconds())
		m.sidebar.lastTokens = len(msg.text) / 4
		m.updateChatViewport()
		return m, nil

	case errMsg:
		if msg.Gen != m.streamGen {
			return m, nil
		}
		m.releaseStream()
		m.messages = append(m.messages, msg.text)
		m.displayMessages = append(m.displayMessages, ErrorStyle.Render(msg.text))
		m.thinking = false
		m.isStreaming = false
		m.liveMsg = ""
		m.restoreFailedPrompt()
		m.updateChatViewport()
		return m, nil

	// ── streaming ────────────────────────────────────────────────────────────
	case StreamChunkMsg:
		if msg.Gen != m.streamGen {
			return m, nil // stale chunk from an abandoned/superseded turn
		}
		if msg.Chunk != "" {
			m.appendStreamChunk(msg.Chunk)
			m.liveMsg = BillyResponseStyle.Render("[Billy] ") +
				highlightStreamBuffer(m.streamBuffer) + " █"
		}
		m.isStreaming = true
		m.thinking = false
		m.updateChatViewport()
		return m, waitForStream(msg.events, msg.Prompt, msg.Gen)

	case StreamDoneMsg:
		if msg.Gen != m.streamGen {
			return m, nil
		}
		m.releaseStream()
		if msg.FullText != "" {
			rendered := m.renderResponse(msg.FullText)
			m.messages = append(m.messages, "[Billy] "+msg.FullText)
			m.displayMessages = append(m.displayMessages, BillyResponseStyle.Render("[Billy] ")+rendered)
			// Final token estimate from the full text — the same len(text)/4
			// basis the non-streaming path uses, so streamed and blocking turns
			// report the same count for the same reply.
			m.sidebar.lastTokens = len(msg.FullText) / 4
		}
		m.liveMsg = ""
		m.streamBuffer = ""
		m.isStreaming = false
		m.thinking = false
		m.lastLatency = time.Since(m.requestStarted)
		m.sidebar.lastLatency = fmt.Sprintf("%.1fs", m.lastLatency.Seconds())
		m.updateChatViewport()
		return m, nil

	case StreamErrMsg:
		if msg.Gen != m.streamGen {
			return m, nil // stale: turn was abandoned (esc) or superseded
		}
		m.releaseStream()
		m.streamBuffer = ""
		m.isStreaming = false
		m.liveMsg = ""
		m.thinking = true
		// 409 turn-in-progress: a non-streaming retry would 409 again, so do
		// not fall back to /ask. Surface the friendly status and drop the turn.
		if errors.Is(msg.Err, errTurnInProgress) {
			return m.handleTurnInProgress()
		}
		if msg.Prompt == "" {
			m.thinking = false
			m.messages = append(m.messages, "⚠️  streaming error")
			m.displayMessages = append(m.displayMessages, ErrorStyle.Render("⚠️  streaming error"))
			m.restoreFailedPrompt()
			m.updateChatViewport()
			return m, nil
		}
		// /ask/stream failed for another reason — fall back to the blocking
		// /ask path, keeping the same generation so its result stays current.
		return m, ask(m.client, msg.Prompt, msg.Gen)

	// ── 409 turn-in-progress (non-streaming path) ─────────────────────────────
	case turnInProgressMsg:
		if msg.Gen != m.streamGen {
			return m, nil
		}
		return m.handleTurnInProgress()

	// ── sidebar polling ──────────────────────────────────────────────────────
	case sidebarTickMsg:
		m.sidebar.sessionID = m.client.sessionID

		// Runtime + governance counters (one call serves both sections).
		if st, err := m.client.RuntimeStatus(); err == nil {
			m.sidebar.connected = true
			m.sidebar.runtimeOK = true
			m.sidebar.lifecycleState = st.LifecycleState
			m.sidebar.runtimePhase = st.RuntimePhase
			m.sidebar.currentStage = st.CurrentStage
			m.sidebar.targetStage = st.TargetStage
			m.sidebar.executionEnabled = st.ExecutionEnabled

			m.sidebar.govReady = true
			m.sidebar.govAllowed = st.Telemetry.Allowed
			m.sidebar.govRejected = st.Telemetry.Rejected

			// Governance border pulse when a new rejection lands.
			if st.Telemetry.Rejected > m.lastGovRejected {
				m.governanceAlertTicks = 3
				// TUI-generated notice — must NOT carry the "[Billy]" prefix. It is
				// the interface reporting a governance event, not Billy speaking;
				// stamping it "[Billy]" made exportChat/buildMarkdown attribute this
				// local line to Billy, breaking the model.go invariant ("the TUI must
				// never speak AS Billy"). Unprefixed, export drops it and the debug
				// save renders it as a system note.
				notice := "🛡 Action blocked by governance policy."
				m.messages = append(m.messages, notice)
				m.displayMessages = append(m.displayMessages, ErrorStyle.Render(notice))
				m.updateChatViewport()
			}
			m.lastGovRejected = st.Telemetry.Rejected
		} else {
			m.sidebar.connected = false
			m.sidebar.runtimeOK = false
		}

		// LLM provider/model.
		if cfg, err := m.client.LLMConfig(); err == nil {
			m.sidebar.provider = cfg.Provider
			m.sidebar.model = cfg.Model
		}

		// Latest governance denial reason code (most recent by timestamp).
		if recent, err := m.client.ReconciliationRecent(20); err == nil {
			m.sidebar.govReady = true
			var latest GuidanceDecision
			for _, d := range recent {
				if d.Timestamp >= latest.Timestamp {
					latest = d
				}
			}
			if latest.ReasonCode != "" {
				m.sidebar.lastDenialCode = latest.ReasonCode
			}
		}

		// Live work progress: active execution jobs (I125).
		if jobs, err := m.client.ActiveJobs(); err == nil {
			m.sidebar.jobsReady = true
			m.sidebar.activeJobs = jobs
		}

		if m.governanceAlertTicks > 0 {
			m.governanceAlertTicks--
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
		// ── model picker: navigate / select / cancel ──
		if m.modelPickerMode {
			switch msg.String() {
			case "up", "k":
				if m.modelPickerIdx > 0 {
					m.modelPickerIdx--
				}
			case "down", "j":
				if m.modelPickerIdx < len(m.modelOptions)-1 {
					m.modelPickerIdx++
				}
			case "enter":
				opt := m.modelOptions[m.modelPickerIdx]
				m.modelPickerMode = false
				m.saveStatus = "Switching to " + opt.model + "…"
				m.saveStatusTicks = 4
				return m, setModel(m.client, opt.provider, opt.model)
			case "esc", "ctrl+c":
				m.modelPickerMode = false
			}
			return m, nil
		}

		// ── help overlay: any key closes ──
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// ── command palette ──
		if m.commandMode {
			switch msg.String() {
			case "enter":
				execCmd := m.execCommand(m.commandInput.Value())
				m.commandMode = false
				m.commandInput.Reset()
				return m, execCmd
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

		// ── abandon an in-flight turn (esc) ──
		// Only intercepts esc while a turn is running; otherwise esc falls through
		// to its normal chat-pane behavior. Cancels the request context (frees the
		// stream goroutine) and bumps the generation so any late messages from the
		// abandoned turn are ignored. The runtime's one-turn guard means Billy may
		// keep working server-side, so a quick resend can hit a (handled) 409.
		if msg.String() == "esc" && (m.thinking || m.isStreaming) {
			m.releaseStream()
			m.streamGen++
			m.thinking = false
			m.isStreaming = false
			m.liveMsg = ""
			m.streamBuffer = ""
			m.restoreFailedPrompt()
			m.saveStatus = "Abandoned — Billy may still be finishing server-side."
			m.saveStatusTicks = 4
			m.updateChatViewport()
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

				// Clear the startup UI hint (a non-Billy "— Say hi to start. —"
				// line lives only in displayMessages while messages is empty)
				// the moment the real conversation begins.
				if len(m.messages) == 0 {
					m.displayMessages = nil
				}

				m.messages = append(m.messages, "[You] "+userMsg)
				m.displayMessages = append(m.displayMessages, UserInputStyle.Render("[You] "+userMsg))
				m.input.Reset()
				m.lastPrompt = userMsg
				// New turn: bump the generation and open a cancelable, generously
				// bounded context. thinking=true with isStreaming=false lets the
				// animated "Billy is thinking…" spinner show until the first chunk
				// arrives (StreamChunkMsg flips isStreaming on).
				m.releaseStream()
				m.streamGen++
				gen := m.streamGen
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				m.streamCancel = cancel
				m.thinking = true
				m.isStreaming = false
				m.streamBuffer = ""
				m.streamTokens = 0
				m.requestStarted = time.Now()
				m.liveMsg = ""
				m.updateChatViewport()
				return m, askStream(ctx, m.client, userMsg, gen)

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
