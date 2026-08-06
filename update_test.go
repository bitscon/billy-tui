package main

import "testing"

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
