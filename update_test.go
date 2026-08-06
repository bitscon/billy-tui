package main

import (
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
