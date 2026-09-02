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

// clearChat resets the conversation surface — both the raw and displayed
// message logs and any in-flight streaming buffer — then refreshes the
// viewport. Shared by the ":clear" command and the ctrl+l shortcut. A pending
// approval dies with its transcript: its prompt block is gone, so the y/n
// quick keys must not keep answering it invisibly.
func (m *model) clearChat() {
	m.messages = []string{}
	m.displayMessages = []string{}
	m.liveMsg = ""
	m.streamBuffer = ""
	m.pendingApproval = nil
	m.updateChatViewport()
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
		case strings.HasPrefix(raw, brainLinePrefix):
			// Per-turn brain records re-render dim, matching their first append —
			// they are routing bookkeeping, not errors, and never Billy's voice.
			rebuilt = append(rebuilt, DimStyle.Render(raw))
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
		m.refreshInputHeight() // a restored multi-line prompt re-grows the box
	}
}

// restoreQueuedPrompt moves the queued prompt back into an empty input when a
// turn ends without a clean completion — queued text auto-sends only after a
// success, and must never be dropped silently on a failure. Call it BEFORE
// restoreFailedPrompt on failure paths: the queued text is the operator's most
// recent intent, so it wins the input box (the failed prompt stays reachable
// via ↑ history). If the operator has since typed something, the queue is left
// standing rather than clobbering their draft — it still sends after the next
// completed turn.
func (m *model) restoreQueuedPrompt() {
	if m.queuedPrompt == "" || m.input.Value() != "" {
		return
	}
	m.input.SetValue(m.queuedPrompt)
	m.refreshInputHeight() // a restored multi-line prompt re-grows the box
	m.queuedPrompt = ""
}

// approvalPromptBlock renders the unmissable prompt for a reply that awaits the
// operator's yes/no (wire contract §4): what will run, against which server, and
// how to answer. Deliberately unattributed — no "[You]"/"[Billy]" prefix — so no
// capture path ever credits it to Billy; rebuildDisplayMessages re-renders it
// via the loud default branch. Command and Target lines appear only when the
// runtime sent them.
func approvalPromptBlock(a *ApprovalRequest) string {
	if a == nil {
		return ""
	}
	block := "⚠️  APPROVAL NEEDED — " + a.Summary
	if a.Command != "" {
		block += "\n    will run: " + a.Command
	}
	if a.Target != "" {
		block += "\n    against:  " + a.Target
	}
	block += "\n    press y to approve, n to decline — or type a reply"
	return block
}

// recordConductorSurfaces lands a completed reply's conductor surfaces in the
// transcript: the per-turn brain record (dim, contract §3) and, when a reply
// awaits the operator, the approval prompt (loud, contract §4). Both lines are
// unattributed — the TUI must never speak AS Billy, and the clean :export drops
// them while the ctrl+s debug capture keeps them as system notes. Nil fields
// (legacy runtime, unrouted turn) leave the transcript untouched (contract §6).
func (m *model) recordConductorSurfaces(brain *BrainReport, approval *ApprovalRequest) {
	if brain != nil {
		m.lastBrain = brain
		line := brainRecordLine(brain)
		m.messages = append(m.messages, line)
		m.displayMessages = append(m.displayMessages, DimStyle.Render(line))
	}
	if approval != nil && approval.Pending {
		m.pendingApproval = approval
		block := approvalPromptBlock(approval)
		m.messages = append(m.messages, block)
		m.displayMessages = append(m.displayMessages, ErrorStyle.Render(block))
	}
}

// submitPrompt sends text to Billy exactly as an enter-key submission does —
// transcript "[You]" line, history push, generation bump, stream call — so the
// enter key, the y/n approval quick keys, and the queued-prompt auto-send all
// produce identical turns. It does not touch the input box: the caller clears
// it when the text came from there, and an auto-send must not clobber a draft
// the operator is typing. Any submission resolves a pending approval — the
// reply IS the answer, over the unchanged /ask path (contract §4).
func (m *model) submitPrompt(text string) tea.Cmd {
	// push to history ring buffer
	m.inputHistory = append(m.inputHistory, text)
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

	m.messages = append(m.messages, "[You] "+text)
	m.displayMessages = append(m.displayMessages, UserInputStyle.Render("[You] "+text))
	m.lastPrompt = text
	m.pendingApproval = nil
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
	return askStream(ctx, m.client, text, gen)
}

// desiredInputRows returns how many rows the input box should occupy: it grows
// with the number of logical lines the operator has entered (one per ctrl+j
// newline) up to maxInputRows, and is further capped on short terminals so the
// chat viewport never drops below its 4-row floor.
func (m model) desiredInputRows() int {
	rows := m.input.LineCount()
	if rows < 1 {
		rows = 1
	}
	limit := maxInputRows
	if m.ready {
		// Layout below the panes is: input rows + 1 hint line; the panes add a
		// 2-row border and there is 1 status row on top. Keeping vpHeight ≥ 4
		// means input rows ≤ height-8.
		if avail := m.height - 8; avail < limit {
			limit = avail
		}
	}
	if limit < 1 {
		limit = 1
	}
	if rows > limit {
		rows = limit
	}
	return rows
}

// refreshInputHeight grows or shrinks the input box to fit its content and keeps
// the chat viewport height in sync, so the status row, panes, input, and hint row
// always sum to the terminal height. Call after any change to the input content.
func (m *model) refreshInputHeight() {
	rows := m.desiredInputRows()
	if m.input.Height() != rows {
		m.input.SetHeight(rows)
	}
	if !m.ready {
		return
	}
	vpH := m.height - 4 - rows
	if vpH < 4 {
		vpH = 4
	}
	if m.chatViewport.Height != vpH {
		m.chatViewport.Height = vpH
		m.updateChatViewport()
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

// routingModes are the two conductor modes, in ':routing' picker order.
var routingModes = []string{"auto", "pinned"}

// routingModeBlurb is the operator-facing meaning of each mode, shared by the
// picker rows and the switch confirmation so both always tell the same story.
func routingModeBlurb(mode string) string {
	if mode == "auto" {
		return "Billy picks a brain per turn"
	}
	return "every turn uses the pinned model"
}

// setRoutingMode fires the mode-only POST (contract v2 §9). The provider/model
// pin is untouched by design — only the mode changes hands.
func setRoutingMode(c *billyClient, mode string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := c.SetRoutingMode(mode)
		if err != nil {
			return routingSetMsg{err: err}
		}
		return routingSetMsg{cfg: cfg}
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
	case "routing":
		// Capability gate (contract v2 §8): a runtime that never reported a
		// routing mode has no mode-set surface — degrade read-only and send
		// nothing, neither the picker nor a POST.
		if m.routingMode == "" {
			m.saveStatus = "This runtime does not report routing — toggle unavailable"
			m.saveStatusTicks = 4
			return nil
		}
		if len(parts) == 1 {
			// no arg → open the two-row picker preselected on the current mode
			m.routingPickerIdx = 0
			for i, mode := range routingModes {
				if mode == m.routingMode {
					m.routingPickerIdx = i
				}
			}
			m.routingPickerMode = true
			return nil
		}
		mode := strings.ToLower(parts[1])
		if mode != "auto" && mode != "pinned" {
			m.saveStatus = "Usage: :routing [auto|pinned]"
			m.saveStatusTicks = 4
			return nil
		}
		m.saveStatus = "Switching routing…"
		m.saveStatusTicks = 4
		return setRoutingMode(m.client, mode)
	case "clear", "c":
		m.clearChat()
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
			// A pending approval belongs to the old session; the new one must
			// not start with y/n silently answering a question it never asked.
			m.pendingApproval = nil
			short := truncate(m.client.sessionID, 20)
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
	m.restoreQueuedPrompt() // queued text wins the input box over the failed prompt
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

		// Reserve rows below the panes: the input box (1..maxInputRows) + 1
		// hint/command bar. Panes: Height() sets inner height; border adds 2 more
		// → paneOuter = inner+2. Layout: 1(status) + (inner+2)(panes) + N(input) +
		// 1(hint) = inner+N+4 = height → inner = height-4-N. refreshInputHeight
		// (called below) is the authoritative sync; this seeds a sane initial size.
		vpHeight := msg.Height - 4 - m.input.Height()
		if vpHeight < 4 {
			vpHeight = 4
		}
		mdWidth := chatWidth - 4
		if mdWidth < 20 {
			mdWidth = 20
		}
		m.mdRenderer = newMdRenderer(mdWidth)
		m.input.SetWidth(msg.Width - 4)
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
		// A shorter terminal may force a multi-line input to shrink; re-clamp the
		// input height against the new size and finalize the viewport height.
		m.refreshInputHeight()
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
			// routingMode is deliberately untouched here — the config GET on the
			// next sidebar poll is the authority. But under auto routing a switch
			// only sets the pin/home config, which the conductor may override on
			// the very next turn (contract §7) — say so rather than implying the
			// picked model now answers every turn.
			if m.routingMode == "auto" {
				m.saveStatus += " — caution: auto routing is on, this set the pin; the conductor may pick another brain next turn"
			}
			m.sidebar.model = msg.model
			m.sidebar.provider = msg.provider
		}
		m.saveStatusTicks = 4
		return m, nil

	// ── routing mode switch result (Modernization 6) ─────────────────────────
	case routingSetMsg:
		if msg.err != nil {
			m.saveStatus = "⚠️  routing switch failed: " + msg.err.Error()
			m.saveStatusTicks = 4
			return m, nil
		}
		// The response IS the runtime's authoritative config reply (contract v2
		// §9) — adopt its mode now; the 5s poll stays the standing authority
		// afterwards, so a runtime downgrade can never leave a stale mode.
		m.routingMode = msg.cfg.RoutingMode
		m.sidebar.provider = msg.cfg.Provider
		m.sidebar.model = msg.cfg.Model
		if msg.cfg.RoutingMode == "" {
			// A runtime that accepted the POST but reports no mode is legacy as
			// far as display goes (contract §2) — say so rather than inventing.
			m.saveStatus = "Runtime reported no routing mode — showing legacy display"
		} else {
			m.saveStatus = "✓ routing " + msg.cfg.RoutingMode + " — " + routingModeBlurb(msg.cfg.RoutingMode)
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
		m.recordConductorSurfaces(msg.Brain, msg.Approval)
		m.thinking = false
		m.liveMsg = ""
		m.lastLatency = time.Since(m.requestStarted)
		m.sidebar.lastLatency = fmt.Sprintf("%.1fs", m.lastLatency.Seconds())
		m.sidebar.lastTokens = len(msg.text) / 4
		m.updateChatViewport()
		// A clean completion releases the queued prompt (enter-while-busy) as a
		// real submission — the promise made when it was queued.
		if q := m.queuedPrompt; q != "" {
			m.queuedPrompt = ""
			return m, m.submitPrompt(q)
		}
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
		// Failed turn: never auto-send the queued prompt into the wreckage —
		// hand it (or, failing that, the failed prompt) back to the input.
		m.restoreQueuedPrompt()
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
		m.recordConductorSurfaces(msg.Brain, msg.Approval)
		m.liveMsg = ""
		m.streamBuffer = ""
		m.isStreaming = false
		m.thinking = false
		m.lastLatency = time.Since(m.requestStarted)
		m.sidebar.lastLatency = fmt.Sprintf("%.1fs", m.lastLatency.Seconds())
		m.updateChatViewport()
		// A clean completion releases the queued prompt (enter-while-busy) as a
		// real submission — the promise made when it was queued.
		if q := m.queuedPrompt; q != "" {
			m.queuedPrompt = ""
			return m, m.submitPrompt(q)
		}
		return m, nil

	case StreamErrMsg:
		if msg.Gen != m.streamGen {
			return m, nil // stale: turn was abandoned (esc) or superseded
		}
		m.releaseStream()
		partial := m.streamBuffer // chunks already received this turn, if any
		m.streamBuffer = ""
		m.isStreaming = false
		m.liveMsg = ""
		m.thinking = true
		// 409 turn-in-progress: a non-streaming retry would 409 again, so do
		// not fall back to /ask. Surface the friendly status and drop the turn.
		if errors.Is(msg.Err, errTurnInProgress) {
			return m.handleTurnInProgress()
		}
		// The stream died mid-reply: the runtime accepted this turn and its
		// one-turn guard is holding it, so a blocking /ask retry would collide
		// and 409 — presenting a long, mostly-delivered answer as a failure.
		// Keep what arrived, attributed to Billy (it is his text), and say
		// honestly that it may be incomplete. No retry.
		if partial != "" {
			m.thinking = false
			m.messages = append(m.messages, "[Billy] "+partial)
			m.displayMessages = append(m.displayMessages,
				BillyResponseStyle.Render("[Billy] ")+m.renderResponse(partial))
			notice := "⚠️  Stream interrupted — the reply above may be incomplete (" + msg.Err.Error() + ")"
			m.messages = append(m.messages, notice)
			m.displayMessages = append(m.displayMessages, ErrorStyle.Render(notice))
			m.lastLatency = time.Since(m.requestStarted)
			m.sidebar.lastLatency = fmt.Sprintf("%.1fs", m.lastLatency.Seconds())
			m.sidebar.lastTokens = len(partial) / 4
			// Not a clean completion: a queued prompt must not fire against a
			// possibly-incomplete reply — hand it back to the input instead.
			m.restoreQueuedPrompt()
			m.updateChatViewport()
			return m, nil
		}
		if msg.Prompt == "" {
			m.thinking = false
			m.messages = append(m.messages, "⚠️  streaming error")
			m.displayMessages = append(m.displayMessages, ErrorStyle.Render("⚠️  streaming error"))
			m.restoreQueuedPrompt() // queued text wins the input box over the failed prompt
			m.restoreFailedPrompt()
			m.updateChatViewport()
			return m, nil
		}
		// /ask/stream failed before any chunk arrived (typically the endpoint
		// is unavailable and the turn never started server-side), so fall back
		// to the blocking /ask path, keeping the same generation so its result
		// stays current. If the turn HAD started, the fallback's 409 is
		// absorbed by the benign turn-in-progress handler above.
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
			// The GET is authoritative for routing mode, on every poll — "" is a
			// legacy runtime and must read as one (contract §2/§6), so no
			// stale "auto" may survive a runtime downgrade.
			m.routingMode = cfg.RoutingMode
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

		// ── routing picker: navigate / select / cancel (Modernization 6) ──
		if m.routingPickerMode {
			switch msg.String() {
			case "up", "k":
				if m.routingPickerIdx > 0 {
					m.routingPickerIdx--
				}
			case "down", "j":
				if m.routingPickerIdx < len(routingModes)-1 {
					m.routingPickerIdx++
				}
			case "enter":
				mode := routingModes[m.routingPickerIdx]
				m.routingPickerMode = false
				if mode == m.routingMode {
					m.saveStatus = "Routing already " + mode
					m.saveStatusTicks = 4
					return m, nil
				}
				m.saveStatus = "Switching routing…"
				m.saveStatusTicks = 4
				return m, setRoutingMode(m.client, mode)
			case "esc", "ctrl+c":
				m.routingPickerMode = false
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
			// An abandoned turn never completes, so its queued prompt would never
			// auto-send — hand it back (it wins the input box over the abandoned
			// prompt, which stays reachable via ↑ history).
			m.restoreQueuedPrompt()
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
			m.clearChat()
			return m, nil

		case "ctrl+t":
			// Toggle terminal mouse capture. Off (default) leaves native
			// click-drag select-and-copy working; on gives the chat viewport
			// mouse-wheel scroll at the cost of native selection.
			m.mouseCapture = !m.mouseCapture
			if m.mouseCapture {
				m.saveStatus = "🖱 Mouse capture ON — wheel scroll (native text-select off)"
				m.saveStatusTicks = 4
				return m, tea.EnableMouseCellMotion
			}
			m.saveStatus = "🖱 Mouse capture OFF — select & copy text"
			m.saveStatusTicks = 4
			return m, tea.DisableMouse

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
			// ── approval quick keys ──
			// A pending approval answers to a bare y/n — but only when they can
			// mean nothing else: input empty (otherwise they are typing), no turn
			// in flight. Both travel the same path as a typed "yes"/"no" — the
			// reply channel is unchanged chat text (contract §4).
			if m.pendingApproval != nil && !m.thinking && !m.isStreaming && m.input.Value() == "" {
				switch msg.String() {
				case "y":
					return m, m.submitPrompt("yes")
				case "n":
					return m, m.submitPrompt("no")
				}
			}
			switch msg.String() {
			case "enter":
				if m.isStreaming || m.thinking {
					// Queue instead of refusing: a reply typed while Billy is
					// finishing — e.g. a fast "yes" to an approval — must reach
					// him, not die in the input box. One slot; a newer
					// enter-while-busy replaces it, and says so.
					if m.input.Value() == "" {
						return m, nil
					}
					replaced := m.queuedPrompt != ""
					m.queuedPrompt = m.input.Value()
					m.input.Reset()
					m.refreshInputHeight() // collapse a multi-line box back to one row
					m.saveStatus = "⏳ Queued — sends when Billy finishes"
					if replaced {
						m.saveStatus = "⏳ Queued (replaced the earlier queued prompt) — sends when Billy finishes"
					}
					m.saveStatusTicks = 4
					return m, nil
				}
				if m.input.Value() == "" {
					return m, nil
				}
				userMsg := m.input.Value()
				m.input.Reset()
				m.refreshInputHeight() // collapse a multi-line box back to one row
				return m, m.submitPrompt(userMsg)

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
				m.refreshInputHeight() // a recalled multi-line entry re-grows the box
				return m, nil

			case "down":
				if m.historyIdx > 0 {
					m.historyIdx--
					m.input.SetValue(m.inputHistory[len(m.inputHistory)-1-m.historyIdx])
				} else if m.historyIdx == 0 {
					m.historyIdx = -1
					m.input.SetValue(m.draftInput)
				}
				m.refreshInputHeight()
				return m, nil

			case "ctrl+u":
				m.input.Reset()
				m.refreshInputHeight()
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
		// A ctrl+j newline (or a backspace that removes one) changes the line
		// count; regrow/shrink the box and re-sync the viewport height.
		m.refreshInputHeight()
		cmds = append(cmds, inputCmd)
	}

	var vpCmd tea.Cmd
	m.chatViewport, vpCmd = m.chatViewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}
