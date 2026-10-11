package main

import (
	"errors"
	"fmt"
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

// ── brain-floor screen (Modernization 5) ─────────────────────────────────────

// testFloors builds the contract v2 §10 fixture table.
func testFloors() *BrainFloors {
	return &BrainFloors{
		Tiers:        []string{"small", "medium", "large"},
		DefaultFloor: "small",
		Roles:        map[string]string{"chat": "small", "coder": "large", "companion": "small", "sysadmin": "medium"},
	}
}

// :brains opens the screen in a loading state and fires the fetch — nothing is
// assumed about the runtime before the GET answers.
func TestBrainsCommandOpensAndLoads(t *testing.T) {
	m := initialModel(nil)
	cmd := m.execCommand("brains")
	if cmd == nil {
		t.Fatalf(":brains should fire the table fetch")
	}
	if !m.floorMode || m.floors != nil {
		t.Fatalf("screen should open with no table yet: floorMode=%v floors=%v", m.floorMode, m.floors)
	}
}

// A loaded table renders rows in a stable sorted order (Go map iteration is
// random); an unsupported runtime (404) degrades the screen to read-only —
// no rows, an honest notice, and enter sends nothing.
func TestFloorsLoadedSortsRowsAndUnsupportedIsReadOnly(t *testing.T) {
	m := initialModel(nil)
	m.floorMode = true
	next, _ := m.Update(floorsLoadedMsg{floors: testFloors()})
	nm := next.(model)
	want := []string{"chat", "coder", "companion", "sysadmin"}
	if len(nm.floorRoles) != len(want) {
		t.Fatalf("rows = %v, want %v", nm.floorRoles, want)
	}
	for i := range want {
		if nm.floorRoles[i] != want[i] {
			t.Fatalf("rows = %v, want sorted %v", nm.floorRoles, want)
		}
	}

	m2 := initialModel(nil)
	m2.floorMode = true
	next2, _ := m2.Update(floorsLoadedMsg{unsupported: true})
	nm2 := next2.(model)
	if nm2.floors != nil {
		t.Fatalf("unsupported must leave no table")
	}
	if !strings.Contains(nm2.floorNotice, "read-only") {
		t.Fatalf("notice should say read-only: %q", nm2.floorNotice)
	}
	next3, cmd := nm2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter on an unavailable table must send nothing")
	}
	if !next3.(model).floorMode {
		t.Fatalf("enter must not close the screen")
	}
}

// Enter on a role opens the tier picker preselected on that role's current
// floor, offering exactly the runtime's tier vocabulary.
func TestFloorEnterOpensTierPickAtCurrentTier(t *testing.T) {
	m := initialModel(nil)
	m.floorMode = true
	next, _ := m.Update(floorsLoadedMsg{floors: testFloors()})
	nm := next.(model)
	// select "sysadmin" (last row of the sorted order)
	nm.floorIdx = len(nm.floorRoles) - 1
	next2, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm2 := next2.(model)
	if cmd != nil {
		t.Fatalf("opening the tier pick must not write anything")
	}
	if !nm2.floorTierPick {
		t.Fatalf("tier picker should be open")
	}
	if nm2.floors.Tiers[nm2.floorTierIdx] != "medium" {
		t.Fatalf("tier preselect = %q, want sysadmin's current floor medium", nm2.floors.Tiers[nm2.floorTierIdx])
	}
}

// In the tier picker, enter on a DIFFERENT tier fires the one-role write; enter
// on the current tier writes nothing; esc backs out to the table.
func TestFloorTierPickWriteSkipAndCancel(t *testing.T) {
	open := func() model {
		m := initialModel(nil)
		m.floorMode = true
		next, _ := m.Update(floorsLoadedMsg{floors: testFloors()})
		nm := next.(model)
		nm.floorIdx = len(nm.floorRoles) - 1 // sysadmin (medium)
		next2, _ := nm.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return next2.(model)
	}

	nm := open()
	next, _ := nm.Update(tea.KeyMsg{Type: tea.KeyDown}) // medium → large
	next2, cmd := next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("picking a different tier should fire the write")
	}
	if next2.(model).floorTierPick {
		t.Fatalf("tier picker should close on enter")
	}

	nm2 := open()
	_, cmd2 := nm2.Update(tea.KeyMsg{Type: tea.KeyEnter}) // enter on current (medium)
	if cmd2 != nil {
		t.Fatalf("picking the current tier must not write")
	}

	nm3 := open()
	next3, _ := nm3.Update(tea.KeyMsg{Type: tea.KeyEsc})
	nm3b := next3.(model)
	if nm3b.floorTierPick || !nm3b.floorMode {
		t.Fatalf("esc should back out to the table, not close the screen")
	}
}

// A successful write adopts the runtime's post-write table — the screen shows
// what actually took effect, selection kept on the changed role — and says so.
// A failed write keeps the table and reports the failure.
func TestFloorSetMsgAdoptsPostWriteTableOrReportsFailure(t *testing.T) {
	m := initialModel(nil)
	m.floorMode = true
	next, _ := m.Update(floorsLoadedMsg{floors: testFloors()})
	nm := next.(model)

	updated := testFloors()
	updated.Roles["sysadmin"] = "large"
	next2, _ := nm.Update(floorSetMsg{role: "sysadmin", tier: "large", floors: updated})
	nm2 := next2.(model)
	if nm2.floors.Roles["sysadmin"] != "large" {
		t.Fatalf("post-write table not adopted: %v", nm2.floors.Roles)
	}
	if nm2.floorRoles[nm2.floorIdx] != "sysadmin" {
		t.Fatalf("selection should stay on the changed role, got %q", nm2.floorRoles[nm2.floorIdx])
	}
	if !strings.Contains(nm2.floorNotice, "✓ sysadmin floor → large") {
		t.Fatalf("notice = %q, want the took-effect confirmation", nm2.floorNotice)
	}

	next3, _ := nm2.Update(floorSetMsg{role: "coder", tier: "small", err: errors.New("400")})
	nm3 := next3.(model)
	if nm3.floors.Roles["coder"] != "large" {
		t.Fatalf("a failed write must not touch the table: %v", nm3.floors.Roles)
	}
	if !strings.Contains(nm3.floorNotice, "⚠ change failed") {
		t.Fatalf("notice = %q, want the failure report", nm3.floorNotice)
	}
}

// esc from the role table closes the whole screen.
func TestFloorScreenEscCloses(t *testing.T) {
	m := initialModel(nil)
	m.floorMode = true
	next, _ := m.Update(floorsLoadedMsg{floors: testFloors()})
	next2, _ := next.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next2.(model).floorMode {
		t.Fatalf("esc should close the floor screen")
	}
}

// ── the pending-question picker (contract v3 §14/§15) ────────────────────────

// testConsentQuestion is a cloud-read consent question: four options, no free
// text, a kind whose answer the runtime matches by number.
func testConsentQuestion() *PendingQuestion {
	return &PendingQuestion{
		QuestionID:    "consentq-1",
		Question:      "Reading your Kanboard board means sending it to the cloud brain. What should I do?",
		Options:       []string{"Allow once", "Allow for this conversation", "Always allow", "Don't allow"},
		AllowFreeText: false,
		Kind:          "cloud_read_consent",
		Source:        "board",
	}
}

// questionModel builds a model the way the real client reaches the picker: a
// sized terminal — the overlay must be drawable before it owns the keyboard —
// and a question armed on the session the turn ran on.
func questionModel(t *testing.T, q *PendingQuestion) model {
	t.Helper()
	m := initialModel(&billyClient{sessionID: "tui-1"})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = next.(model)
	m.pendingQuestion = q
	m.questionSession = "tui-1"
	m.turnSession = "tui-1"
	m.questionPickerMode = true
	return m
}

// A reply carrying a question opens the picker on the first option and records
// the session it arrived on. A newer reply REPLACES it; a reply carrying none
// closes it (contract v3 §15 rule 4).
func TestPendingQuestionOpensPickerAndNewerReplyReplacesIt(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	next, _ := m.Update(responseMsg{text: "pick one", Question: testConsentQuestion(), Gen: 0})
	nm := next.(model)
	if nm.pendingQuestion == nil || !nm.questionPickerMode {
		t.Fatalf("picker did not open: q=%+v open=%v", nm.pendingQuestion, nm.questionPickerMode)
	}
	if nm.questionPickerIdx != 0 {
		t.Fatalf("picker must open on the first option, got idx %d", nm.questionPickerIdx)
	}
	if nm.questionSession != "tui-1" {
		t.Fatalf("questionSession = %q, want %q", nm.questionSession, "tui-1")
	}

	// The question is NOT copied into the transcript — the runtime's own reply
	// text already carries it and its numbered list.
	for _, line := range nm.messages {
		if strings.Contains(line, "Allow for this conversation") && !strings.HasPrefix(line, "[Billy] ") {
			t.Fatalf("picker must not add its own transcript block: %q", line)
		}
	}

	// A newer reply with a different question replaces the open one.
	nm.questionPickerIdx = 3
	newer := &PendingQuestion{QuestionID: "q-2", Question: "Which one?", Options: []string{"a", "b"}, Kind: "question"}
	next2, _ := nm.Update(responseMsg{text: "and now this", Question: newer, Gen: 0})
	nm2 := next2.(model)
	if nm2.pendingQuestion == nil || nm2.pendingQuestion.QuestionID != "q-2" {
		t.Fatalf("newer reply did not replace the question: %+v", nm2.pendingQuestion)
	}
	if nm2.questionPickerIdx != 0 {
		t.Fatalf("a replaced picker must reset the selection, got idx %d", nm2.questionPickerIdx)
	}

	// A reply carrying no question closes the picker outright.
	next3, _ := nm2.Update(responseMsg{text: "all done", Gen: 0})
	nm3 := next3.(model)
	if nm3.pendingQuestion != nil || nm3.questionPickerMode {
		t.Fatalf("a reply with no question must close the picker: q=%+v open=%v", nm3.pendingQuestion, nm3.questionPickerMode)
	}
}

// The same holds on the streaming path — the question rides its own frame but
// lands on the done message.
func TestStreamDoneMsgOpensPicker(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	next, _ := m.Update(StreamDoneMsg{FullText: "pick one", Question: testConsentQuestion(), Gen: 0})
	nm := next.(model)
	if nm.pendingQuestion == nil || !nm.questionPickerMode {
		t.Fatalf("streamed question did not open the picker")
	}
}

// Arrow keys and j/k move the selection and clamp at both ends; nothing is sent
// while navigating.
func TestQuestionPickerNavigationClamps(t *testing.T) {
	m := questionModel(t, testConsentQuestion())

	step := func(mm model, key string) model {
		next, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil {
			t.Fatalf("navigation must not send anything (key %q)", key)
		}
		return next.(model)
	}
	m = step(m, "k") // already at the top
	if m.questionPickerIdx != 0 {
		t.Fatalf("up at the top must clamp, got %d", m.questionPickerIdx)
	}
	m = step(m, "j")
	m = step(m, "j")
	if m.questionPickerIdx != 2 {
		t.Fatalf("idx = %d, want 2", m.questionPickerIdx)
	}
	m = step(m, "j")
	m = step(m, "j") // already at the bottom
	if m.questionPickerIdx != 3 {
		t.Fatalf("down at the bottom must clamp, got %d", m.questionPickerIdx)
	}
	m = step(m, "k")
	if m.questionPickerIdx != 2 {
		t.Fatalf("idx = %d, want 2", m.questionPickerIdx)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatalf("arrow navigation must not send anything")
	}
	if next.(model).questionPickerIdx != 3 {
		t.Fatalf("down arrow did not move the selection")
	}
}

// Enter sends the selected option's NUMBER, on the same session, as an ordinary
// submission: a [You] line, a real turn, and the picker closed so the choice
// cannot be sent twice.
func TestQuestionPickerEnterSendsTheOptionNumber(t *testing.T) {
	m := questionModel(t, testConsentQuestion())
	m.questionPickerIdx = 2 // "Always allow" → "3"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd == nil {
		t.Fatalf("enter must send the answer")
	}
	if got := nm.messages[len(nm.messages)-1]; got != "[You] 3" {
		t.Fatalf("sent %q, want %q", got, "[You] 3")
	}
	if nm.lastPrompt != "3" {
		t.Fatalf("lastPrompt = %q, want %q", nm.lastPrompt, "3")
	}
	if nm.client.sessionID != "tui-1" {
		t.Fatalf("the answer must go on the question's own session, got %q", nm.client.sessionID)
	}
	if nm.pendingQuestion != nil || nm.questionPickerMode {
		t.Fatalf("a sent choice must close the picker")
	}
	if !nm.thinking {
		t.Fatalf("the answer must start a real turn")
	}
}

// The number keys choose directly. A key beyond the option list does nothing at
// all — it never lands on some other option.
func TestQuestionPickerNumberKeys(t *testing.T) {
	m := questionModel(t, testConsentQuestion())

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	nm := next.(model)
	if cmd == nil {
		t.Fatalf("a number key must send the answer")
	}
	if got := nm.messages[len(nm.messages)-1]; got != "[You] 4" {
		t.Fatalf("sent %q, want %q", got, "[You] 4")
	}

	m2 := questionModel(t, &PendingQuestion{QuestionID: "q-1", Question: "Which?", Options: []string{"a", "b"}, Kind: "question"})
	next2, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	nm2 := next2.(model)
	if cmd2 != nil {
		t.Fatalf("a number past the option list must send nothing")
	}
	if len(nm2.messages) != 0 || nm2.questionPickerIdx != 0 || !nm2.questionPickerMode {
		t.Fatalf("a number past the list must change nothing: msgs=%q idx=%d open=%v",
			nm2.messages, nm2.questionPickerIdx, nm2.questionPickerMode)
	}
}

// Esc closes the picker and SENDS NOTHING. The question stays answerable, so
// the state survives and a typed number still works (contract v3 §15 rule 3).
func TestQuestionPickerEscSendsNothing(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		m := questionModel(t, testConsentQuestion())
		m.questionPickerIdx = 1

		next, cmd := m.Update(key)
		nm := next.(model)
		if cmd != nil {
			t.Fatalf("%v must send nothing", key)
		}
		if len(nm.messages) != 0 {
			t.Fatalf("%v must not put anything in the transcript: %q", key, nm.messages)
		}
		if nm.questionPickerMode {
			t.Fatalf("%v must close the picker", key)
		}
		if nm.pendingQuestion == nil {
			t.Fatalf("%v must leave the question answerable", key)
		}
		if nm.thinking || nm.isStreaming {
			t.Fatalf("%v must not start a turn", key)
		}
	}
}

// A permission request and a read-only lift are the two kinds the runtime
// fences to a TYPED answer — a number answers neither honestly. The picker
// sends the option label, verbatim, for exactly those two.
func TestQuestionPickerFencedKindsSendTheLabel(t *testing.T) {
	cases := []struct {
		kind string
		opts []string
		want string
	}{
		{questionKindPermission, []string{"allow once", "allow for this session", "deny"}, "allow for this session"},
		{questionKindLift, []string{"approve", "keep it read-only"}, "keep it read-only"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			m := questionModel(t, &PendingQuestion{
				QuestionID: "q-1", Question: "May I?", Options: tc.opts, Kind: tc.kind,
			})
			m.questionPickerIdx = 1

			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			nm := next.(model)
			if cmd == nil {
				t.Fatalf("enter must send the answer")
			}
			if got := nm.messages[len(nm.messages)-1]; got != "[You] "+tc.want {
				t.Fatalf("sent %q, want %q — the label shown must be the text sent", got, "[You] "+tc.want)
			}
		})
	}
}

// A fenced kind whose label is itself all digits cannot be expressed on the
// wire (the runtime would read it as a position), so the picker sends NOTHING
// and hands the label to the input box for the operator to send.
func TestQuestionPickerFencedAllDigitLabelSendsNothing(t *testing.T) {
	m := questionModel(t, &PendingQuestion{
		QuestionID: "q-1", Question: "Which port?", Options: []string{"8080", "9090"},
		Kind: questionKindPermission,
	})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("an ambiguous label must not be sent")
	}
	if len(nm.messages) != 0 {
		t.Fatalf("nothing must reach the transcript: %q", nm.messages)
	}
	if nm.input.Value() != "8080" {
		t.Fatalf("input = %q, want the label %q", nm.input.Value(), "8080")
	}
	if nm.saveStatus != questionTypeItNotice {
		t.Fatalf("status = %q, want the type-it notice", nm.saveStatus)
	}
}

// An answer is only ever sent on the session the question arrived on. After
// ':session new' the picker sends nothing and says why.
func TestQuestionPickerRefusesAChangedSession(t *testing.T) {
	m := questionModel(t, testConsentQuestion())
	m.client.sessionID = "tui-2" // ':session new' has moved the client on

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("a choice must never be sent on another session")
	}
	if len(nm.messages) != 0 {
		t.Fatalf("nothing must reach the transcript: %q", nm.messages)
	}
	if nm.pendingQuestion != nil || nm.questionPickerMode {
		t.Fatalf("the stale question must be dropped")
	}
	if nm.saveStatus != questionSessionMovedNotice {
		t.Fatalf("status = %q, want the session-moved notice", nm.saveStatus)
	}
}

// A choice is not sent into a turn that is still running.
func TestQuestionPickerRefusesWhileBusy(t *testing.T) {
	m := questionModel(t, testConsentQuestion())
	m.isStreaming = true

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil || len(nm.messages) != 0 {
		t.Fatalf("a choice must not be sent while a turn is in flight")
	}
	if nm.saveStatus != questionBusyNotice {
		t.Fatalf("status = %q, want the busy notice", nm.saveStatus)
	}
}

// ':session new' and a cleared transcript both drop the question — the runtime
// matches an answer only on its own session, and a cleared transcript no longer
// shows the question the picker refers to.
func TestSessionNewAndClearDropThePendingQuestion(t *testing.T) {
	for _, cmdText := range []string{"session new", "clear"} {
		m := initialModel(&billyClient{sessionID: "tui-1"})
		m.pendingQuestion = testConsentQuestion()
		m.questionSession = "tui-1"
		m.questionPickerMode = true

		m.execCommand(cmdText)
		if m.pendingQuestion != nil || m.questionPickerMode || m.questionSession != "" {
			t.Fatalf(":%s must drop the pending question: q=%+v open=%v session=%q",
				cmdText, m.pendingQuestion, m.questionPickerMode, m.questionSession)
		}
	}
}

// A typed submission closes the picker too, so one question can never be
// answered twice.
func TestTypedSubmissionClosesThePicker(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	m.pendingQuestion = testConsentQuestion()
	m.questionSession = "tui-1"
	m.questionPickerMode = false // operator pressed esc, then typed
	m.input.SetValue("2")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd == nil {
		t.Fatalf("a typed answer must still submit")
	}
	if got := nm.messages[len(nm.messages)-1]; got != "[You] 2" {
		t.Fatalf("sent %q, want %q", got, "[You] 2")
	}
	if nm.pendingQuestion != nil {
		t.Fatalf("a submission must close the open question")
	}
}

// ':permissions' sends exactly '/permissions' as the whole message — the
// runtime's own recognizer accepts nothing else. An argument is refused locally
// and sends nothing.
func TestPermissionsCommandSendsExactlyThatMessage(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	cmd := m.execCommand("permissions")
	if cmd == nil {
		t.Fatalf(":permissions must send a turn")
	}
	if got := m.messages[len(m.messages)-1]; got != "[You] /permissions" {
		t.Fatalf("sent %q, want %q", got, "[You] /permissions")
	}
	if m.lastPrompt != "/permissions" {
		t.Fatalf("lastPrompt = %q, want %q", m.lastPrompt, "/permissions")
	}

	m2 := initialModel(&billyClient{sessionID: "tui-1"})
	if cmd := m2.execCommand("permissions board"); cmd != nil {
		t.Fatalf("an argument must not be sent to the runtime")
	}
	if len(m2.messages) != 0 {
		t.Fatalf("nothing must reach the transcript: %q", m2.messages)
	}
	if m2.saveStatus != "Usage: :permissions" {
		t.Fatalf("status = %q, want the usage line", m2.saveStatus)
	}

	m3 := initialModel(&billyClient{sessionID: "tui-1"})
	m3.thinking = true
	if cmd := m3.execCommand("permissions"); cmd != nil {
		t.Fatalf(":permissions must not fire into a running turn")
	}
	if m3.saveStatus != permissionsBusyNotice {
		t.Fatalf("status = %q, want the busy notice", m3.saveStatus)
	}
}

// answerForOption is the one place a choice becomes wire text. This pins every
// row of the contract's §15 rule 1.
func TestAnswerForOption(t *testing.T) {
	ordinary := &PendingQuestion{Options: []string{"Allow once", "Don't allow"}, Kind: "cloud_read_consent"}
	perm := &PendingQuestion{Options: []string{"allow once", "deny"}, Kind: questionKindPermission}
	lift := &PendingQuestion{Options: []string{"approve", "no"}, Kind: questionKindLift}
	digits := &PendingQuestion{Options: []string{"8080", "9090"}, Kind: questionKindPermission}

	escape := &PendingQuestion{Options: []string{"approve\x1b[2J", "no"}, Kind: questionKindLift}

	cases := []struct {
		name      string
		q         *PendingQuestion
		idx       int
		want      string
		sendable  bool
		stageable bool
	}{
		{"ordinary first", ordinary, 0, "1", true, true},
		{"ordinary second", ordinary, 1, "2", true, true},
		{"permission label", perm, 1, "deny", true, true},
		{"lift label", lift, 0, "approve", true, true},
		{"fenced all-digit label", digits, 0, "8080", false, true},
		{"fenced label with an escape sequence", escape, 0, "approve\x1b[2J", false, false},
		{"nil question", nil, 0, "", false, false},
		{"index below range", ordinary, -1, "", false, false},
		{"index past range", ordinary, 2, "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, sendable, stageable := answerForOption(tc.q, tc.idx)
			if got != tc.want || sendable != tc.sendable || stageable != tc.stageable {
				t.Fatalf("answerForOption = (%q, %v, %v), want (%q, %v, %v)",
					got, sendable, stageable, tc.want, tc.sendable, tc.stageable)
			}
		})
	}
}

// A fenced label carrying control characters is REFUSED, not echoed. Its bytes
// would otherwise reach the transcript, the resize redraw and the input history,
// none of which sanitise for display — so choosing it would clear the operator's
// own screen on their own keypress.
func TestFencedLabelWithControlCharactersIsRefusedNotEchoed(t *testing.T) {
	m := questionModel(t, &PendingQuestion{
		QuestionID: "q-1", Question: "Lift the hold?",
		Options: []string{"approve\x1b[2J\x1b[H", "keep it read-only"},
		Kind:    questionKindLift,
	})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("an unsafe label must not be sent")
	}
	if nm.input.Value() != "" {
		t.Fatalf("an unsafe label must not be staged in the input box: %q", nm.input.Value())
	}
	for _, line := range nm.messages {
		if strings.Contains(line, "\x1b") {
			t.Fatalf("an escape sequence reached the transcript: %q", line)
		}
	}
	if nm.saveStatus != questionUnsafeLabelNotice {
		t.Fatalf("status = %q, want the unsafe-label notice", nm.saveStatus)
	}
}

// questionContext keeps everything the reply said AROUND the question and drops
// only the question line and the runtime's own numbered option lines.
func TestQuestionContextKeepsTheProseAndDropsTheList(t *testing.T) {
	q := &PendingQuestion{
		QuestionID: "q-1",
		Question:   "Which one would you like to change?",
		Options:    []string{"your Kanboard board", "your files"},
		Kind:       "cloud_read_permissions",
	}
	message := strings.Join([]string{
		"What the cloud brain may read, and what it may not:",
		"- your Kanboard board: asks each time",
		"- your files: always allowed",
		"",
		"Which one would you like to change?",
		"",
		"1. your Kanboard board",
		"2. your files",
		"",
		"Reply with the number or the option itself.",
	}, "\n")

	got := questionContext(message, q)
	for _, want := range []string{
		"What the cloud brain may read",
		"- your Kanboard board: asks each time",
		"- your files: always allowed",
		"Reply with the number or the option itself.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("context dropped %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Which one would you like to change?") {
		t.Fatalf("the question must not be repeated in the context:\n%s", got)
	}
	for _, unwanted := range []string{"1. your Kanboard board", "2. your files"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("the numbered list must not survive in the context:\n%s", got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("blank runs must be collapsed:\n%q", got)
	}

	// A numbered line whose label is NOT the option at that position is prose
	// and is kept.
	kept := questionContext("1. something else entirely\nQuestion?", q)
	if !strings.Contains(kept, "1. something else entirely") {
		t.Fatalf("a non-matching numbered line must be kept: %q", kept)
	}
	if questionContext("anything", nil) != "" {
		t.Fatalf("no question means no context")
	}
}

// The reply's surrounding prose travels with the question, so the overlay cannot
// hide what the runtime deliberately kept out of the question text.
func TestAdoptedQuestionCarriesTheReplyProse(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = sized.(model)

	q := &PendingQuestion{
		QuestionID: "q-1", Question: "Lift the hold?",
		Options: []string{"approve", "keep it read-only"},
		Kind:    questionKindLift,
	}
	message := "This conversation is read-only. Answering 'approve' is what lets me make changes.\n\nLift the hold?\n\n1. approve\n2. keep it read-only"
	next, _ := m.Update(responseMsg{text: message, Question: q, Gen: 0})
	nm := next.(model)
	if !strings.Contains(nm.questionContext, "read-only") {
		t.Fatalf("the lift warning did not travel with the question: %q", nm.questionContext)
	}
	if !strings.Contains(nm.View(), "This conversation is read-only") {
		t.Fatalf("the overlay hides the lift warning:\n%s", nm.View())
	}

	// A reply with no question clears it.
	next2, _ := nm.Update(responseMsg{text: "done", Gen: 0})
	if next2.(model).questionContext != "" {
		t.Fatalf("the context must die with the question")
	}
}

// Every picker refusal CLOSES the picker and reports on the status bar, which is
// visible again the moment the overlay is gone. A notice drawn inside the
// overlay had to be budgeted for, and budgeting for it either made the whole
// picker vanish on a short terminal or cost the operator the prose he was meant
// to read. The question stays answerable from the input box, exactly as after
// esc.
func TestPickerRefusalClosesThePickerAndIsVisible(t *testing.T) {
	m := questionModel(t, testConsentQuestion())
	m.isStreaming = true // forces the busy refusal

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("a busy refusal must send nothing")
	}
	if nm.questionPickerMode {
		t.Fatalf("a refusal must close the picker so its notice is visible")
	}
	if nm.pendingQuestion == nil {
		t.Fatalf("the question must stay answerable")
	}
	if nm.saveStatus != questionBusyNotice {
		t.Fatalf("status = %q, want the busy notice", nm.saveStatus)
	}
	if !strings.Contains(nm.View(), "still working") {
		t.Fatalf("the notice is not visible:\n%s", nm.View())
	}
}

// The degradation matrix (contract v3 §17), row by row, from the reply the
// client receives to what it does about it.
func TestPendingQuestionDegradationMatrix(t *testing.T) {
	cases := []struct {
		row        string
		question   *PendingQuestion
		session    string // the client's session when the choice is made
		wantPicker bool
		wantSent   string // "" = nothing goes on the wire
	}{
		{
			row:        "legacy runtime / ordinary turn — nothing pending",
			question:   nil,
			session:    "tui-1",
			wantPicker: false,
		},
		{
			row:        "kind takes a number — the number is sent",
			question:   testConsentQuestion(),
			session:    "tui-1",
			wantPicker: true,
			wantSent:   "1",
		},
		{
			row:        "fenced kind — the label is sent, never a number",
			question:   &PendingQuestion{QuestionID: "q", Question: "May I?", Options: []string{"allow once", "deny"}, Kind: questionKindPermission},
			session:    "tui-1",
			wantPicker: true,
			wantSent:   "allow once",
		},
		{
			row:        "fenced kind, all-digit label — nothing is sent",
			question:   &PendingQuestion{QuestionID: "q", Question: "Which?", Options: []string{"8080", "9090"}, Kind: questionKindPermission},
			session:    "tui-1",
			wantPicker: true,
		},
		{
			row:        "session moved on — nothing is sent",
			question:   testConsentQuestion(),
			session:    "tui-9",
			wantPicker: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.row, func(t *testing.T) {
			m := initialModel(&billyClient{sessionID: "tui-1"})
			sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
			m = sized.(model)
			next, _ := m.Update(responseMsg{text: "reply", Question: tc.question, Gen: 0})
			m = next.(model)
			if m.questionPickerMode != tc.wantPicker {
				t.Fatalf("picker open = %v, want %v", m.questionPickerMode, tc.wantPicker)
			}
			if !tc.wantPicker {
				return
			}
			m.client.sessionID = tc.session
			before := len(m.messages)
			next2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m2 := next2.(model)
			if tc.wantSent == "" {
				if cmd != nil || len(m2.messages) != before {
					t.Fatalf("expected nothing on the wire, got cmd=%v messages=%q", cmd != nil, m2.messages[before:])
				}
				return
			}
			if cmd == nil {
				t.Fatalf("expected %q to be sent, nothing was", tc.wantSent)
			}
			if got := m2.messages[len(m2.messages)-1]; got != "[You] "+tc.wantSent {
				t.Fatalf("sent %q, want %q", got, "[You] "+tc.wantSent)
			}
		})
	}
}

// ── self-review regressions (contract v3) ────────────────────────────────────

// ':session new' is reachable from the colon palette WHILE Billy is thinking,
// so the in-flight reply still lands after the client has moved on. The question
// it carries belongs to the session the TURN ran on, not to the new one — so the
// picker refuses it and says so, instead of sending an answer onto a session the
// runtime has nothing to match it against.
func TestSessionNewMidTurnDoesNotRestampTheQuestion(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = sized.(model)

	m.submitPrompt("read my board") // the turn runs on tui-1
	m.execCommand("session new")    // the operator moves on mid-turn
	if m.client.sessionID == "tui-1" {
		t.Fatalf(":session new did not change the session")
	}

	next, _ := m.Update(responseMsg{text: "pick one", Question: testConsentQuestion(), Gen: m.streamGen})
	nm := next.(model)
	if nm.questionSession != "tui-1" {
		t.Fatalf("questionSession = %q, want the turn's own session %q", nm.questionSession, "tui-1")
	}

	before := len(nm.messages)
	next2, cmd := nm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm2 := next2.(model)
	if cmd != nil || len(nm2.messages) != before {
		t.Fatalf("an answer must not go out on a session the question never belonged to")
	}
	if nm2.saveStatus != questionSessionMovedNotice {
		t.Fatalf("status = %q, want the session-moved notice", nm2.saveStatus)
	}
}

// Below the two-pane layout's floor View() draws only the resize notice, so the
// picker must not own the keyboard: enter would otherwise send a consent answer
// chosen off a screen that never showed the question.
func TestQuestionPickerOwnsNoKeysWhenItCannotBeDrawn(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = sized.(model)
	m.pendingQuestion = testConsentQuestion()
	m.questionSession = "tui-1"
	m.turnSession = "tui-1"
	m.questionPickerMode = true
	if m.paneLayoutFits() {
		t.Fatalf("40x20 should not clear the layout floor")
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("an undrawable picker must send nothing")
	}
	for _, line := range nm.messages {
		if strings.HasPrefix(line, "[You] ") {
			t.Fatalf("an undrawable picker put an answer on the wire: %q", line)
		}
	}
}

// The "it is in the input box" notice must be TRUE. With a draft in the box the
// label is left alone and the notice says to type it instead.
func TestFencedLabelNoticeMatchesWhatActuallyHappened(t *testing.T) {
	q := &PendingQuestion{
		QuestionID: "q-1", Question: "Which port?", Options: []string{"8080", "9090"},
		Kind: questionKindPermission,
	}

	m := questionModel(t, q)
	m.input.SetValue("half a sentence")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("nothing should be sent")
	}
	if nm.input.Value() != "half a sentence" {
		t.Fatalf("a draft must not be clobbered, got %q", nm.input.Value())
	}
	if nm.saveStatus != questionTypeItYourselfNotice {
		t.Fatalf("status = %q, want the type-it-yourself notice", nm.saveStatus)
	}

	m2 := questionModel(t, q)
	next2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm2 := next2.(model)
	if nm2.input.Value() != "8080" || nm2.saveStatus != questionTypeItNotice {
		t.Fatalf("empty box: input = %q status = %q", nm2.input.Value(), nm2.saveStatus)
	}
}

// What a choice SENDS is the bytes the runtime sent, control characters and all
// — the display sanitiser must never reach the wire, or the runtime's own
// label match would fail.
func TestFencedLabelIsSentByteExact(t *testing.T) {
	// A non-breaking space: ordinary text the sanitiser leaves alone, so the
	// label is sendable and the wire bytes can be compared exactly.
	label := "allow\u00a0once"
	m := questionModel(t, &PendingQuestion{
		QuestionID: "q-1", Question: "May I?", Options: []string{label, "deny"},
		Kind: questionKindPermission,
	})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd == nil {
		t.Fatalf("enter must send the answer")
	}
	if got := nm.messages[len(nm.messages)-1]; got != "[You] "+label {
		t.Fatalf("sent %q, want the label byte-exact %q", got, "[You] "+label)
	}
}

// A number key must never answer with an option the window has scrolled off
// screen: the operator would be choosing a label they cannot see.
func TestQuestionNumberKeyIgnoresAnOffScreenOption(t *testing.T) {
	many := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		many = append(many, fmt.Sprintf("option number %d", i+1))
	}
	m := questionModel(t, &PendingQuestion{
		QuestionID: "q-1", Question: "Which?", Options: many, Kind: "question",
	})
	m.questionPickerIdx = 25

	plan := m.questionOverlayPlan()
	if !plan.fits {
		t.Fatalf("the overlay should be drawable at 100x32")
	}
	start, _ := questionOptionWindow(25, 40, plan.optionRows)
	if start == 0 {
		t.Fatalf("the list should be windowed away from option 1 for this test")
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("a number naming an off-screen option must send nothing")
	}
	if len(nm.messages) != 0 {
		t.Fatalf("nothing must reach the transcript: %q", nm.messages)
	}
	if nm.questionPickerIdx != 25 {
		t.Fatalf("the selection must not move, got %d", nm.questionPickerIdx)
	}
	// And the key line does not offer the numbers while the list is windowed.
	if strings.Contains(m.View(), "1–") {
		t.Fatalf("the key line offers numbers for a windowed list:\n%s", m.View())
	}
}

// A terminal that clears the two-pane floor but is still too short to draw the
// picker whole owns no picker keys either: the gate is the render plan, not the
// pane layout.
func TestQuestionPickerOwnsNoKeysWhenTheOverlayWouldNotFit(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m = sized.(model)
	m.pendingQuestion = &PendingQuestion{
		QuestionID: "q-1", Question: "May I write that file?",
		Options: []string{"allow once", "allow for this session", "deny"},
		Kind:    questionKindPermission, Tool: "write_file", Target: "notes/today.md",
	}
	m.questionSession = "tui-1"
	m.turnSession = "tui-1"
	m.questionPickerMode = true

	if !m.paneLayoutFits() {
		t.Fatalf("100x12 clears the two-pane floor")
	}
	if m.questionOverlayPlan().fits {
		t.Fatalf("a permission question needs more than 12 rows")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(model)
	if cmd != nil {
		t.Fatalf("an undrawable picker must send nothing")
	}
	for _, line := range nm.messages {
		if strings.HasPrefix(line, "[You] ") {
			t.Fatalf("an undrawable picker put an answer on the wire: %q", line)
		}
	}
	if strings.Contains(m.View(), questionPickerTitle) {
		t.Fatalf("an undrawable picker must not be drawn:\n%s", m.View())
	}
}

// ── self-review wave 5 regressions ──────────────────────────────────────────

// The question's own rows come first: the prose that travels with it must not
// squeeze the sentence being answered down to a clipped line while there is
// room for it. It bit hardest on the read-only lift question, which is the one
// privilege escalation in this feature.
func TestQuestionKeepsItsRowsAgainstTheProse(t *testing.T) {
	q := &PendingQuestion{
		QuestionID: "q-1",
		Question:   "Shall I lift the hold so I can make changes in this conversation, or keep it read-only?",
		Options:    []string{"approve", "keep it read-only"},
		Kind:       questionKindLift,
	}
	prose := strings.Join([]string{
		"This conversation is read-only.",
		"Answering 'approve' — typed, not numbered — is what lets me make changes in it.",
		"Any other answer keeps it read-only.",
		"Reply with the option itself, typed out.",
	}, "\n")

	for _, size := range [][2]int{{80, 21}, {80, 24}, {100, 18}, {120, 30}} {
		m := initialModel(&billyClient{sessionID: "tui-1"})
		sized, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = sized.(model)
		m.pendingQuestion = q
		m.questionContext = prose
		m.questionPickerMode = true

		plan := m.questionOverlayPlan()
		if !plan.fits {
			continue
		}
		need := m.renderedLineCount(q.Question)
		if plan.questionRows < need {
			t.Fatalf("%dx%d: question got %d of the %d rows it needs (context got %d)\n%s",
				size[0], size[1], plan.questionRows, need, plan.contextRows, m.View())
		}
		if plan.contextRows < 2 {
			t.Fatalf("%dx%d: the warning was dropped entirely, not clipped\n%s", size[0], size[1], m.View())
		}
	}
}

// The overlay never draws the client's global status line. A notice about an
// export, a model switch or an abandoned turn has no business on the screen a
// consent choice is sent from — and the picker's own refusals report there
// instead, after closing the picker, so the status bar is visible again.
func TestGlobalStatusNoticesStayOffTheConsentScreen(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = sized.(model)
	m.saveStatus = "✓ Exported → /somewhere/chat.md"
	m.saveStatusTicks = 4

	next, _ := m.Update(responseMsg{text: "pick one", Question: testConsentQuestion(), Gen: 0})
	nm := next.(model)
	if !nm.questionPickerMode {
		t.Fatalf("the picker should be open")
	}
	if strings.Contains(nm.View(), "Exported") {
		t.Fatalf("an unrelated status notice is drawn on the consent screen:\n%s", nm.View())
	}
}

// A question that is itself several lines is dropped from the context whole, so
// it is never rendered twice.
func TestMultiLineQuestionIsNotDuplicatedInTheContext(t *testing.T) {
	q := &PendingQuestion{
		QuestionID: "q-1",
		Question:   "Reading your board means sending it to the cloud brain.\nWhat should I do?",
		Options:    []string{"Allow once", "Don't allow"},
		Kind:       "cloud_read_consent",
	}
	message := strings.Join([]string{
		"Here is some prose.",
		"",
		"Reading your board means sending it to the cloud brain.",
		"What should I do?",
		"",
		"1. Allow once",
		"2. Don't allow",
		"",
		"Reply with the number or the option itself.",
	}, "\n")

	got := questionContext(message, q)
	if strings.Contains(got, "What should I do?") {
		t.Fatalf("the multi-line question survived into the context:\n%s", got)
	}
	if !strings.Contains(got, "Here is some prose.") || !strings.Contains(got, "Reply with the number") {
		t.Fatalf("the context lost real prose:\n%s", got)
	}

	// CRLF line endings are handled the same way.
	if crlf := questionContext(strings.ReplaceAll(message, "\n", "\r\n"), q); strings.Contains(crlf, "What should I do?") {
		t.Fatalf("CRLF message: the question survived:\n%s", crlf)
	}
}

// ── self-review wave 6 regressions ──────────────────────────────────────────

// Rows reserved for the "N more" indicators go back to the content when the
// whole list turns out to be on screen. Reserving and never returning them
// dropped the consent screen's prose while two rows sat empty.
func TestReservedMoreRowsGoBackWhenTheListIsWhole(t *testing.T) {
	q := testConsentQuestion() // four options: the reserve kicks in
	prose := "Your board is local only right now.\nNothing has left this machine yet."

	for _, w := range []int{60, 80, 100, 160, 240} {
		m := initialModel(&billyClient{sessionID: "tui-1"})
		sized, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 16})
		m = sized.(model)
		m.pendingQuestion = q
		m.questionContext = prose
		m.questionPickerMode = true

		plan := m.questionOverlayPlan()
		if !plan.fits {
			t.Fatalf("%dx16: the consent picker should fit", w)
		}
		if plan.optionRows != len(q.Options) {
			t.Fatalf("%dx16: only %d of %d options get rows", w, plan.optionRows, len(q.Options))
		}
		if plan.contextRows < 2 {
			t.Fatalf("%dx16: the prose was dropped entirely (context=%d question=%d)\n%s",
				w, plan.contextRows, plan.questionRows, m.View())
		}
		if rows := strings.Count(m.View(), "\n") + 1; rows > 16 {
			t.Fatalf("%dx16: %d rows rendered\n%s", w, rows, m.View())
		}
	}
}

// Nothing is clipped or dropped while a row sits empty, and the plan never
// promises more rows than the overlay has.
func TestPlanSpendsEveryRowItHas(t *testing.T) {
	questions := []*PendingQuestion{
		testConsentQuestion(),
		{QuestionID: "q", Question: strings.Repeat("a long question. ", 40),
			Options: []string{"approve", "no"}, Kind: questionKindLift},
		{QuestionID: "q", Question: "May I?", Options: []string{"allow once", "deny"},
			Kind: questionKindPermission, Tool: "write_file", Target: "notes/today.md"},
	}
	proses := []string{"", "one line of prose", strings.Repeat("prose line\n", 40)}
	for _, q := range questions {
		for _, prose := range proses {
			for _, w := range []int{60, 100, 160} {
				for h := 8; h <= 60; h++ {
					m := initialModel(&billyClient{sessionID: "tui-1"})
					sized, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
					m = sized.(model)
					m.pendingQuestion = q
					m.questionContext = prose
					m.questionPickerMode = true

					plan := m.questionOverlayPlan()
					if !plan.fits {
						continue
					}
					spent := HelpOverlayStyle.GetVerticalFrameSize() + 3 + plan.subjectRows + 1 +
						plan.questionRows + plan.optionRows + plan.noteRows + plan.contextRows
					start, end := questionOptionWindow(m.questionPickerIdx, len(q.Options), plan.optionRows)
					if start > 0 {
						spent++
					}
					if end < len(q.Options) {
						spent++
					}
					if spent > h {
						t.Fatalf("%dx%d: the plan promises %d rows", w, h, spent)
					}
					// A clipped question, or prose dropped outright, is only
					// honest when there was genuinely nothing left to give.
					if plan.questionRows < m.renderedLineCount(q.Question) && spent < h {
						t.Fatalf("%dx%d: question clipped to %d with %d rows unused",
							w, h, plan.questionRows, h-spent)
					}
					if prose != "" && plan.contextRows == 0 && spent <= h-2 {
						t.Fatalf("%dx%d: prose dropped with %d rows unused", w, h, h-spent)
					}
				}
			}
		}
	}
}

// The prose keeps its own line structure: every newline in it was put there by
// the client's own filter, and flattening them ran the /permissions settings
// listing into one unreadable paragraph.
func TestProseKeepsItsLinesOnScreen(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-1"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = sized.(model)
	m.pendingQuestion = &PendingQuestion{
		QuestionID: "q-1", Question: "Which one would you like to change?",
		Options: []string{"your Kanboard board", "your files"},
		Kind:    "cloud_read_permissions",
	}
	m.questionContext = strings.Join([]string{
		"What the cloud brain may read, and what it may not:",
		"- your Kanboard board: asks each time",
		"- your files: always allowed",
	}, "\n")
	m.questionPickerMode = true

	out := m.View()
	for _, want := range []string{
		"- your Kanboard board: asks each time",
		"- your files: always allowed",
	} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("the listing was flattened — %q is not on a line of its own:\n%s", want, out)
		}
	}
}

// An option label that is itself several lines is dropped from the prose block
// whole, so no bare "1. …" survives to read like a forged list entry.
func TestMultiLineOptionLabelLeavesNoForgedListLine(t *testing.T) {
	q := &PendingQuestion{
		QuestionID: "q-1", Question: "Lift the hold?",
		Options: []string{"approve\nnow", "keep it read-only"},
		Kind:    questionKindLift,
	}
	message := "Lift the hold?\n\n1. approve\nnow\n2. keep it read-only\n\nReply with the option itself."
	got := questionContext(message, q)
	for _, unwanted := range []string{"1. approve", "2. keep it read-only"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("a numbered line survived into the prose: %q\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "Reply with the option itself.") {
		t.Fatalf("the closing line was lost:\n%s", got)
	}
}

// A label the runtime padded still matches, so no padded numbered line is left
// standing in the prose block.
func TestPaddedOptionLabelIsStillDroppedFromTheContext(t *testing.T) {
	q := &PendingQuestion{
		QuestionID: "q-1", Question: "Lift the hold?",
		Options: []string{" approve", "keep it read-only"},
		Kind:    questionKindLift,
	}
	got := questionContext("Lift the hold?\n\n1.  approve\n2. keep it read-only\n\nReply with the option itself.", q)
	for _, unwanted := range []string{"1.", "2."} {
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, unwanted) {
				t.Fatalf("a numbered line survived into the prose: %q\n%s", line, got)
			}
		}
	}
	if !strings.Contains(got, "Reply with the option itself.") {
		t.Fatalf("the closing line was lost:\n%s", got)
	}
}
