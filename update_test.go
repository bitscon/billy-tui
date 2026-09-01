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
