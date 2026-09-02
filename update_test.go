package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The live streaming token estimate must derive from the whole buffer (len/4),
// not a running per-chunk sum. Regression for the old "+= len(chunk)/4 + 1",
// whose per-chunk +1 inflated the count.
func TestAppendStreamChunkEstimatesFromWholeBuffer(t *testing.T) {
	m := &model{}
	chunks := []string{"Hello ", "world", " this is a longer chunk"}
	var full string
	for _, c := range chunks {
		m.appendStreamChunk(c)
		full += c
	}
	if m.streamBuffer != full {
		t.Fatalf("streamBuffer = %q, want %q", m.streamBuffer, full)
	}
	if want := len(full) / 4; m.streamTokens != want {
		t.Fatalf("streamTokens = %d, want %d (len=%d)", m.streamTokens, want, len(full))
	}
}

// A streamed turn and a non-streamed turn must report the same token estimate
// for the same reply — both use len(text)/4. The old per-chunk accounting made
// the streamed count drift above the non-streamed one.
func TestStreamTokenEstimateMatchesNonStreaming(t *testing.T) {
	reply := "The quick brown fox jumps over the lazy dog, and then some."
	m := &model{}
	for _, r := range reply { // stream one rune at a time
		m.appendStreamChunk(string(r))
	}
	nonStreaming := len(reply) / 4
	if m.streamTokens != nonStreaming {
		t.Fatalf("streamed estimate = %d, non-streamed estimate = %d", m.streamTokens, nonStreaming)
	}
}

// The input box grows one row per logical line and is capped at maxInputRows so
// a very long multi-line prompt cannot swallow the chat pane.
func TestDesiredInputRowsGrowsAndCaps(t *testing.T) {
	m := initialModel(nil)
	m.ready = true
	m.height = 40 // ample room, so only maxInputRows caps growth

	if got := m.desiredInputRows(); got != 1 {
		t.Fatalf("empty input rows = %d, want 1", got)
	}
	m.input.SetValue("a\nb\nc")
	if got := m.desiredInputRows(); got != 3 {
		t.Fatalf("3-line input rows = %d, want 3", got)
	}
	m.input.SetValue(strings.Repeat("x\n", 20)) // 21 logical lines
	if got := m.desiredInputRows(); got != maxInputRows {
		t.Fatalf("21-line input rows = %d, want %d (capped)", got, maxInputRows)
	}
}

// On a short terminal the input is clamped harder than maxInputRows so the chat
// viewport keeps its 4-row floor (rows ≤ height-8).
func TestDesiredInputRowsClampedOnShortTerminal(t *testing.T) {
	m := initialModel(nil)
	m.ready = true
	m.height = 12 // height-8 = 4, so at most 4 input rows
	m.input.SetValue(strings.Repeat("x\n", 20))
	if got := m.desiredInputRows(); got != 4 {
		t.Fatalf("rows on 12-row terminal = %d, want 4", got)
	}
}

// refreshInputHeight must keep the chat viewport height in lockstep with the
// input height so the whole layout still sums to the terminal height.
func TestRefreshInputHeightSyncsViewport(t *testing.T) {
	m := initialModel(nil)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = tm.(model)

	baseVP := m.chatViewport.Height // input is 1 row here → 40-4-1 = 35
	if baseVP != 35 {
		t.Fatalf("base viewport height = %d, want 35", baseVP)
	}
	m.input.SetValue("one\ntwo\nthree") // 3 rows
	m.refreshInputHeight()

	if m.input.Height() != 3 {
		t.Fatalf("input height = %d, want 3", m.input.Height())
	}
	if want := 40 - 4 - 3; m.chatViewport.Height != want {
		t.Fatalf("viewport height = %d, want %d", m.chatViewport.Height, want)
	}
}

// ctrl+t flips mouse capture and emits a bubbletea mouse command each time:
// off→on returns EnableMouseCellMotion (wheel scroll), on→off DisableMouse
// (native text-select restored). Default is off so selection works out of box.
func TestCtrlTTogglesMouseCapture(t *testing.T) {
	m := initialModel(nil)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	if tm.(model).mouseCapture {
		t.Fatalf("mouse capture should start OFF (native text-select works)")
	}

	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if !tm.(model).mouseCapture {
		t.Fatalf("ctrl+t should turn mouse capture ON")
	}
	if cmd == nil {
		t.Fatalf("enabling capture should return a mouse command")
	}

	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if tm.(model).mouseCapture {
		t.Fatalf("a second ctrl+t should turn mouse capture back OFF")
	}
	if cmd == nil {
		t.Fatalf("disabling capture should return a mouse command")
	}
}

// ctrl+j inserts a newline (rather than submitting or being swallowed) and the
// box grows a row — the end-to-end proof the multi-line input works.
func TestCtrlJInsertsNewlineAndGrows(t *testing.T) {
	m := initialModel(nil)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = tm.(model)

	if got := m.input.Value(); got != "a\nb" {
		t.Fatalf("input value = %q, want %q", got, "a\nb")
	}
	if m.input.Height() != 2 {
		t.Fatalf("input height = %d, want 2 after one newline", m.input.Height())
	}
}

// The mid-reply failure path (Modernization 8): once chunks have arrived the
// runtime's one-turn guard owns the turn, so the client must NOT fall back to
// the blocking /ask — that retry would 409 and present a mostly-delivered
// answer as a failure. Instead it keeps the partial text, attributed to Billy,
// with an honest unattributed interruption notice, and returns no command.
func TestStreamErrAfterChunksKeepsPartialAndDoesNotRetry(t *testing.T) {
	m := initialModel(nil)
	m.isStreaming = true
	m.streamBuffer = "partial reply text"

	next, cmd := m.Update(StreamErrMsg{Prompt: "the prompt", Err: errors.New("token too long"), Gen: 0})
	if cmd != nil {
		t.Fatalf("expected no fallback command after chunks arrived, got one")
	}
	nm := next.(model)
	if nm.thinking || nm.isStreaming {
		t.Fatalf("turn should be finished: thinking=%v isStreaming=%v", nm.thinking, nm.isStreaming)
	}
	if nm.streamBuffer != "" {
		t.Fatalf("streamBuffer not cleared: %q", nm.streamBuffer)
	}
	var gotPartial, gotNotice bool
	for _, msg := range nm.messages {
		if msg == "[Billy] partial reply text" {
			gotPartial = true
		}
		if strings.HasPrefix(msg, "⚠️") && strings.Contains(msg, "interrupted") {
			gotNotice = true
		}
	}
	if !gotPartial {
		t.Fatalf("partial text not kept as a Billy message: %q", nm.messages)
	}
	if !gotNotice {
		t.Fatalf("no honest interruption notice recorded: %q", nm.messages)
	}
}

// A stream that fails before ANY chunk arrived still falls back to the
// blocking /ask — the pre-existing recovery for an unavailable stream
// endpoint stays intact.
func TestStreamErrBeforeChunksStillFallsBack(t *testing.T) {
	m := initialModel(nil)
	next, cmd := m.Update(StreamErrMsg{Prompt: "the prompt", Err: errors.New("connection refused"), Gen: 0})
	if cmd == nil {
		t.Fatalf("expected the /ask fallback command, got nil")
	}
	nm := next.(model)
	if !nm.thinking {
		t.Fatalf("expected thinking=true while the fallback runs")
	}
}

// ── conductor surfaces: per-reply brain record (Modernization 3) ─────────────

// testBrain is a representative routed-turn report for the transcript tests.
var testBrain = &BrainReport{
	Placement: "home",
	Provider:  "ollama",
	ModelID:   "qwen3.5:9b",
	Reason:    "routine turn; floor small; resolved at home",
}

// A non-streaming reply that carries a brain report lands the record right
// after the Billy line — as its own unattributed [Brain] line, dim in the
// display — and updates lastBrain.
func TestResponseMsgAppendsBrainRecord(t *testing.T) {
	m := initialModel(nil)
	next, _ := m.Update(responseMsg{text: "hello there", Brain: testBrain, Gen: 0})
	nm := next.(model)

	if nm.lastBrain != testBrain {
		t.Fatalf("lastBrain not set from the reply's report")
	}
	want := brainRecordLine(testBrain)
	if len(nm.messages) != 2 || nm.messages[0] != "[Billy] hello there" || nm.messages[1] != want {
		t.Fatalf("messages = %q, want Billy reply then %q", nm.messages, want)
	}
	if !strings.HasPrefix(nm.messages[1], brainLinePrefix) {
		t.Fatalf("brain record lost its prefix: %q", nm.messages[1])
	}
	if got := nm.displayMessages[len(nm.displayMessages)-1]; got != DimStyle.Render(want) {
		t.Fatalf("brain record displayed as %q, want dim %q", got, DimStyle.Render(want))
	}
}

// The streamed path records the brain report the same way the blocking path
// does, so both transports leave identical transcripts for the same turn.
func TestStreamDoneMsgAppendsBrainRecord(t *testing.T) {
	m := initialModel(nil)
	next, _ := m.Update(StreamDoneMsg{FullText: "streamed reply", Brain: testBrain, Gen: 0})
	nm := next.(model)

	if nm.lastBrain != testBrain {
		t.Fatalf("lastBrain not set from the stream's report")
	}
	want := brainRecordLine(testBrain)
	if len(nm.messages) != 2 || nm.messages[0] != "[Billy] streamed reply" || nm.messages[1] != want {
		t.Fatalf("messages = %q, want Billy reply then %q", nm.messages, want)
	}
}

// A reply with no brain report (legacy runtime, unrouted turn) must leave the
// transcript exactly as today — no invented record (contract §1/§6).
func TestNilBrainLeavesTranscriptUnchanged(t *testing.T) {
	m := initialModel(nil)
	next, _ := m.Update(responseMsg{text: "plain reply", Gen: 0})
	nm := next.(model)
	if len(nm.messages) != 1 || nm.messages[0] != "[Billy] plain reply" {
		t.Fatalf("nil-Brain responseMsg altered the transcript: %q", nm.messages)
	}
	if nm.lastBrain != nil {
		t.Fatalf("lastBrain invented without a report")
	}

	m2 := initialModel(nil)
	next2, _ := m2.Update(StreamDoneMsg{FullText: "plain streamed", Gen: 0})
	nm2 := next2.(model)
	if len(nm2.messages) != 1 || nm2.messages[0] != "[Billy] plain streamed" {
		t.Fatalf("nil-Brain StreamDoneMsg altered the transcript: %q", nm2.messages)
	}
}

// On a resize re-flow, [Brain] lines must re-render dim — not fall into the
// error-styled default — and must never gain a Billy attribution.
func TestRebuildDisplayMessagesStylesBrainDim(t *testing.T) {
	m := initialModel(nil)
	line := brainRecordLine(testBrain)
	m.messages = []string{"[You] hi", "[Billy] hello", line}
	m.rebuildDisplayMessages()

	if len(m.displayMessages) != 3 {
		t.Fatalf("rebuilt %d display lines, want 3", len(m.displayMessages))
	}
	if got, want := m.displayMessages[2], DimStyle.Render(line); got != want {
		t.Fatalf("brain line rebuilt as %q, want dim %q", got, want)
	}
	if strings.Contains(m.displayMessages[2], "[Billy]") {
		t.Fatalf("brain line attributed to Billy on rebuild: %q", m.displayMessages[2])
	}
}

// ── conductor surfaces: approval affordance (Modernization 4) ────────────────

func testApproval() *ApprovalRequest {
	return &ApprovalRequest{
		Pending: true,
		ID:      "appr-1",
		Summary: "restart nginx",
		Command: "systemctl restart nginx",
		Target:  "barn",
	}
}

// A reply that awaits the operator sets pendingApproval and appends an
// unattributed prompt block showing what will run, against which server, and
// how to answer. A non-pending approval object changes nothing.
func TestApprovalPendingSetsStateAndRendersBlock(t *testing.T) {
	m := initialModel(nil)
	next, _ := m.Update(responseMsg{text: "Reply yes to run it.", Approval: testApproval(), Gen: 0})
	nm := next.(model)

	if nm.pendingApproval == nil || nm.pendingApproval.ID != "appr-1" {
		t.Fatalf("pendingApproval not set from the reply")
	}
	block := nm.messages[len(nm.messages)-1]
	for _, want := range []string{"restart nginx", "systemctl restart nginx", "barn", "press y to approve, n to decline"} {
		if !strings.Contains(block, want) {
			t.Fatalf("approval block missing %q:\n%s", want, block)
		}
	}
	if strings.HasPrefix(block, "[Billy] ") || strings.HasPrefix(block, "[You] ") {
		t.Fatalf("approval block must be unattributed: %q", block)
	}

	m2 := initialModel(nil)
	next2, _ := m2.Update(responseMsg{text: "done", Approval: &ApprovalRequest{Pending: false, ID: "appr-2"}, Gen: 0})
	nm2 := next2.(model)
	if nm2.pendingApproval != nil || len(nm2.messages) != 1 {
		t.Fatalf("non-pending approval must change nothing: pending=%v messages=%q", nm2.pendingApproval, nm2.messages)
	}
}

// With an approval pending, an empty input, and no turn in flight, a bare y/n
// answers through the same path as a typed submission: [You] line, cleared
// pending state, and a real turn command.
func TestApprovalQuickKeysSubmitYesNo(t *testing.T) {
	for key, want := range map[string]string{"y": "yes", "n": "no"} {
		m := initialModel(nil)
		m.messages = []string{"[Billy] Reply yes to run it."}
		m.pendingApproval = testApproval()

		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		nm := next.(model)
		if cmd == nil {
			t.Fatalf("%q should submit a turn command", key)
		}
		if got := nm.messages[len(nm.messages)-1]; got != "[You] "+want {
			t.Fatalf("%q transcript line = %q, want %q", key, got, "[You] "+want)
		}
		if nm.pendingApproval != nil {
			t.Fatalf("submission must clear pendingApproval")
		}
		if !nm.thinking {
			t.Fatalf("%q should start a real turn", key)
		}
	}
}

// Without a pending approval y is just a letter; with one pending but text
// already in the input it is still just a letter — no hidden submissions.
func TestYTypesNormallyWhenNotAnAnswer(t *testing.T) {
	m := initialModel(nil)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	nm := next.(model)
	if nm.input.Value() != "y" {
		t.Fatalf("input = %q, want %q (typed normally)", nm.input.Value(), "y")
	}
	if len(nm.messages) != 0 {
		t.Fatalf("no submission expected: %q", nm.messages)
	}

	m2 := initialModel(nil)
	m2.pendingApproval = testApproval()
	m2.input.SetValue("h")
	next2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	nm2 := next2.(model)
	if nm2.input.Value() != "hy" {
		t.Fatalf("input = %q, want %q (typing continues)", nm2.input.Value(), "hy")
	}
	if nm2.pendingApproval == nil {
		t.Fatalf("typing must not resolve the approval")
	}
}

// ── queue instead of refusing (Modernization 4) ──────────────────────────────

// Enter while a turn is in flight queues the prompt instead of refusing it:
// input cleared, nothing sent, status says so. A second enter-while-busy
// replaces the queued prompt and says that too.
func TestEnterWhileBusyQueuesPrompt(t *testing.T) {
	m := initialModel(nil)
	m.thinking = true
	m.input.SetValue("yes")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("queueing must not send a turn")
	}
	if nm.queuedPrompt != "yes" {
		t.Fatalf("queuedPrompt = %q, want %q", nm.queuedPrompt, "yes")
	}
	if nm.input.Value() != "" {
		t.Fatalf("input not cleared after queueing: %q", nm.input.Value())
	}
	if len(nm.messages) != 0 {
		t.Fatalf("nothing may reach the transcript at queue time: %q", nm.messages)
	}
	if !strings.Contains(nm.saveStatus, "Queued") {
		t.Fatalf("status should say the prompt queued: %q", nm.saveStatus)
	}

	nm.input.SetValue("no wait, no")
	next2, _ := nm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm2 := next2.(model)
	if nm2.queuedPrompt != "no wait, no" {
		t.Fatalf("second enter should replace the queue: %q", nm2.queuedPrompt)
	}
	if !strings.Contains(nm2.saveStatus, "replaced") {
		t.Fatalf("status should say the queue was replaced: %q", nm2.saveStatus)
	}
}

// A queued prompt auto-sends when the turn completes cleanly — on both the
// streamed and blocking completion paths — as a full submission: [You] line
// after Billy's reply, a new turn command, and an emptied queue.
func TestQueuedPromptAutoSendsOnCompletion(t *testing.T) {
	m := initialModel(nil)
	m.isStreaming = true
	m.queuedPrompt = "queued question"

	next, cmd := m.Update(StreamDoneMsg{FullText: "done reply", Gen: 0})
	nm := next.(model)
	if cmd == nil {
		t.Fatalf("queued prompt should auto-send on StreamDoneMsg")
	}
	if nm.queuedPrompt != "" {
		t.Fatalf("queue not emptied: %q", nm.queuedPrompt)
	}
	if len(nm.messages) != 2 || nm.messages[0] != "[Billy] done reply" || nm.messages[1] != "[You] queued question" {
		t.Fatalf("messages = %q, want Billy reply then the queued [You] line", nm.messages)
	}
	if !nm.thinking {
		t.Fatalf("auto-send should start a real turn")
	}

	m2 := initialModel(nil)
	m2.thinking = true
	m2.queuedPrompt = "second queued"
	next2, cmd2 := m2.Update(responseMsg{text: "ok", Gen: 0})
	nm2 := next2.(model)
	if cmd2 == nil || nm2.messages[len(nm2.messages)-1] != "[You] second queued" {
		t.Fatalf("queued prompt should auto-send on responseMsg too: %q", nm2.messages)
	}
}

// A failed turn must NOT fire the queued prompt into the wreckage: it moves
// back into the (empty) input instead — winning the box over the failed
// prompt, which stays reachable via ↑ history — and nothing is sent.
func TestQueuedPromptNotSentOnErrMsg(t *testing.T) {
	m := initialModel(nil)
	m.thinking = true
	m.lastPrompt = "the failed prompt"
	m.queuedPrompt = "queued text"

	next, cmd := m.Update(errMsg{text: "⚠️  boom", Gen: 0})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("a failed turn must not auto-send the queue")
	}
	if nm.queuedPrompt != "" {
		t.Fatalf("queue should be drained into the input: %q", nm.queuedPrompt)
	}
	if nm.input.Value() != "queued text" {
		t.Fatalf("input = %q, want the queued text (queued wins over the failed prompt)", nm.input.Value())
	}
	for _, msg := range nm.messages {
		if msg == "[You] queued text" {
			t.Fatalf("queued text must not reach the transcript on failure")
		}
	}
}

// ── routing-mode glue (Modernization 2) ──────────────────────────────────────

// The sidebar-tick glue (routingMode from GET /api/v1/llm/config) needs a live
// client — the tick handler dereferences it for four endpoints — so the
// initialModel(nil) pattern cannot reach it; the decode itself is covered by
// TestLLMConfigRoutingMode. What IS reachable is the modelSetMsg half: a switch
// under auto routing must warn that it only set the pin (contract §7), a
// legacy/pinned runtime must not warn, and neither may touch routingMode (the
// poll is the authority).
func TestModelSetMsgAutoRoutingCaution(t *testing.T) {
	m := initialModel(nil)
	m.routingMode = "auto"
	next, _ := m.Update(modelSetMsg{provider: "ollama", model: "qwen3.5:9b"})
	nm := next.(model)
	if !strings.Contains(nm.saveStatus, "auto routing is on") || !strings.Contains(nm.saveStatus, "pin") {
		t.Fatalf("auto-mode switch status missing the pin caution: %q", nm.saveStatus)
	}
	if nm.routingMode != "auto" {
		t.Fatalf("modelSetMsg must not touch routingMode: %q", nm.routingMode)
	}

	m2 := initialModel(nil) // legacy runtime: routingMode ""
	next2, _ := m2.Update(modelSetMsg{provider: "ollama", model: "qwen3.5:9b"})
	nm2 := next2.(model)
	if strings.Contains(nm2.saveStatus, "auto routing") {
		t.Fatalf("legacy runtime must keep today's plain status: %q", nm2.saveStatus)
	}
	if nm2.routingMode != "" {
		t.Fatalf("modelSetMsg must not invent a routing mode: %q", nm2.routingMode)
	}
}

// ── mode toggle (Modernization 6) ────────────────────────────────────────────

// Against a runtime that never reported a routing mode there is no mode-set
// surface: :routing — bare or with an argument — must degrade read-only,
// opening nothing and sending nothing (contract v2 §8 capability gate).
func TestRoutingCommandDegradesOnLegacyRuntime(t *testing.T) {
	m := initialModel(nil) // routingMode "" = legacy
	if cmd := m.execCommand("routing"); cmd != nil {
		t.Fatalf("bare :routing must send nothing on a legacy runtime")
	}
	if m.routingPickerMode {
		t.Fatalf("picker must not open on a legacy runtime")
	}
	if !strings.Contains(m.saveStatus, "unavailable") {
		t.Fatalf("status should say the toggle is unavailable: %q", m.saveStatus)
	}
	if cmd := m.execCommand("routing pinned"); cmd != nil {
		t.Fatalf(":routing pinned must send nothing on a legacy runtime")
	}
}

// Bare :routing on a conductor-aware runtime opens the picker preselected on
// the current mode, without POSTing anything yet.
func TestRoutingCommandOpensPickerAtCurrentMode(t *testing.T) {
	m := initialModel(nil)
	m.routingMode = "pinned"
	if cmd := m.execCommand("routing"); cmd != nil {
		t.Fatalf("opening the picker must not send anything")
	}
	if !m.routingPickerMode {
		t.Fatalf("picker should be open")
	}
	if routingModes[m.routingPickerIdx] != "pinned" {
		t.Fatalf("picker preselect = %q, want the current mode", routingModes[m.routingPickerIdx])
	}
}

// :routing with a valid mode argument fires the switch; an invalid argument is
// refused with a usage hint and nothing sent.
func TestRoutingCommandArgSwitchesOrRefuses(t *testing.T) {
	m := initialModel(nil)
	m.routingMode = "auto"
	if cmd := m.execCommand("routing pinned"); cmd == nil {
		t.Fatalf(":routing pinned should fire the switch command")
	}

	m2 := initialModel(nil)
	m2.routingMode = "auto"
	if cmd := m2.execCommand("routing sideways"); cmd != nil {
		t.Fatalf("an invalid mode must send nothing")
	}
	if !strings.Contains(m2.saveStatus, "auto|pinned") {
		t.Fatalf("status should show usage: %q", m2.saveStatus)
	}
}

// In the picker, enter on a DIFFERENT mode fires the switch and closes; enter
// on the current mode closes without a pointless POST; esc cancels.
func TestRoutingPickerEnterAndCancel(t *testing.T) {
	m := initialModel(nil)
	m.routingMode = "auto"
	m.execCommand("routing") // opens preselected on auto (idx 0)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next, cmd := next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd == nil {
		t.Fatalf("selecting the other mode should fire the switch")
	}
	if nm.routingPickerMode {
		t.Fatalf("picker should close on enter")
	}

	m2 := initialModel(nil)
	m2.routingMode = "auto"
	m2.execCommand("routing")
	next2, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyEnter}) // enter on the current mode
	if cmd2 != nil {
		t.Fatalf("selecting the current mode must not POST")
	}
	if !strings.Contains(next2.(model).saveStatus, "already") {
		t.Fatalf("status should say already in that mode: %q", next2.(model).saveStatus)
	}

	m3 := initialModel(nil)
	m3.routingMode = "auto"
	m3.execCommand("routing")
	next3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next3.(model).routingPickerMode {
		t.Fatalf("esc should close the picker")
	}
}

// A successful mode set adopts the runtime's authoritative reply — the mode
// flips immediately, not on the next poll — and the status confirms it in
// operator words. A failed set keeps the current mode and says so.
func TestRoutingSetMsgAdoptsReplyOrKeepsModeOnError(t *testing.T) {
	m := initialModel(nil)
	m.routingMode = "auto"
	next, _ := m.Update(routingSetMsg{cfg: &LLMConfig{Provider: "ollama", Model: "qwen3.5:9b", RoutingMode: "pinned"}})
	nm := next.(model)
	if nm.routingMode != "pinned" {
		t.Fatalf("routingMode = %q, want pinned (adopted from the reply)", nm.routingMode)
	}
	if !strings.Contains(nm.saveStatus, "routing pinned") {
		t.Fatalf("status = %q, want the pinned confirmation", nm.saveStatus)
	}

	m2 := initialModel(nil)
	m2.routingMode = "auto"
	next2, _ := m2.Update(routingSetMsg{err: errors.New("boom")})
	nm2 := next2.(model)
	if nm2.routingMode != "auto" {
		t.Fatalf("a failed switch must keep the current mode, got %q", nm2.routingMode)
	}
	if !strings.Contains(nm2.saveStatus, "failed") {
		t.Fatalf("status should report the failure: %q", nm2.saveStatus)
	}
}
