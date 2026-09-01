package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The model picker must window a long list to the terminal height, keep the
// selected row visible, and clamp to the option count — otherwise a host with
// many models (e.g. Ollama) overflows the screen and the selection can move
// below the fold.
func TestModelPickerWindow(t *testing.T) {
	tests := []struct {
		name               string
		idx, total, h      int
		wantStart, wantEnd int
	}{
		// Short list on a tall terminal: show everything, no windowing.
		{"fits entirely", 0, 5, 40, 0, 5},
		// Long list, selection at top: window anchored at 0.
		{"top of long list", 0, 100, 30, 0, 20},
		// Long list, selection at bottom: window ends at total.
		{"bottom of long list", 99, 100, 30, 80, 100},
		// Long list, selection in the middle: window centered on idx.
		{"middle of long list", 50, 100, 30, 40, 60},
		// Very short terminal still shows at least 3 rows.
		{"short terminal floor", 10, 100, 8, 9, 12},
		// Empty list is safe.
		{"empty", 0, 0, 30, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end := modelPickerWindow(tc.idx, tc.total, tc.h)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("modelPickerWindow(%d,%d,%d) = [%d,%d), want [%d,%d)",
					tc.idx, tc.total, tc.h, start, end, tc.wantStart, tc.wantEnd)
			}
			// The selected index must always fall inside the window (for a
			// non-empty list).
			if tc.total > 0 && (tc.idx < start || tc.idx >= end) {
				t.Fatalf("selected idx %d outside window [%d,%d)", tc.idx, start, end)
			}
			// The window never runs past the option list.
			if end > tc.total {
				t.Fatalf("window end %d exceeds total %d", end, tc.total)
			}
		})
	}
}

// The selected row stays visible as it walks the whole list — the property that
// actually matters for usability, checked across every index.
func TestModelPickerWindowKeepsSelectionVisible(t *testing.T) {
	const total, h = 200, 24
	for idx := 0; idx < total; idx++ {
		start, end := modelPickerWindow(idx, total, h)
		if idx < start || idx >= end {
			t.Fatalf("idx %d not visible in window [%d,%d)", idx, start, end)
		}
		if start < 0 || end > total {
			t.Fatalf("window [%d,%d) out of bounds for total %d", start, end, total)
		}
	}
}

// A legacy runtime reports no routing mode, so the status-bar base must stay
// byte-identical to the pre-conductor display — and "pinned" is truthful today,
// so it keeps the same shape (contract §6).
func TestStatusBaseLegacyAndPinnedUnchanged(t *testing.T) {
	tests := []struct {
		name                  string
		mode                  string
		brain                 *BrainReport
		model, provider, want string
	}{
		{"legacy full", "", nil, "qwen3.5:9b", "ollama", "Billy │ qwen3.5:9b │ ollama"},
		{"legacy empty", "", nil, "", "", "Billy"},
		{"legacy model only", "", nil, "qwen3.5:9b", "", "Billy │ qwen3.5:9b"},
		{"pinned", "pinned", nil, "qwen3.5:9b", "ollama", "Billy │ qwen3.5:9b │ ollama"},
		// A stale brain report never changes the pinned/legacy bar.
		{"pinned ignores brain", "pinned", &BrainReport{Placement: "home", ModelID: "x"}, "qwen3.5:9b", "ollama", "Billy │ qwen3.5:9b │ ollama"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := statusBase(tc.mode, tc.brain, tc.model, tc.provider)
			if got != tc.want {
				t.Fatalf("statusBase(%q) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

// Under auto routing the bar must state the mode — and never present the
// pinned model as the plain answerer. When a brain report exists, the last
// answering model is shown instead.
func TestStatusBaseAuto(t *testing.T) {
	got := statusBase("auto", nil, "qwen-pin", "ollama")
	if got != "Billy │ routing auto" {
		t.Fatalf("auto without brain = %q, want %q", got, "Billy │ routing auto")
	}
	got = statusBase("auto", &BrainReport{Placement: "cloud", ModelID: "gpt-x"}, "qwen-pin", "ollama")
	if got != "Billy │ routing auto │ last: gpt-x" {
		t.Fatalf("auto with brain = %q, want %q", got, "Billy │ routing auto │ last: gpt-x")
	}
	if strings.Contains(got, "qwen-pin") {
		t.Fatalf("auto bar presents the pinned model as the answerer: %q", got)
	}
}

// With a legacy runtime (no routing mode, no brain report) the sidebar must
// carry zero conductor artifacts — the degradation contract (§6).
func TestRenderSidebarLegacyHasNoConductorArtifacts(t *testing.T) {
	s := sidebarState{connected: true, sessionID: "tui-1", provider: "ollama", model: "qwen3.5:9b"}
	out := renderSidebar(s, "", nil, 30, 40)
	for _, want := range []string{"model", "qwen3.5:9b", "provider", "ollama"} {
		if !strings.Contains(out, want) {
			t.Fatalf("legacy sidebar missing %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"routing", "Brain", "local", "cloud", "pin"} {
		if strings.Contains(out, banned) {
			t.Fatalf("legacy sidebar leaks conductor artifact %q:\n%s", banned, out)
		}
	}
}

// A reported routing mode adds the routing line; under auto the config pair is
// relabelled as the pin so the sidebar stops claiming it answers every turn.
func TestRenderSidebarRoutingLine(t *testing.T) {
	s := sidebarState{connected: true, provider: "ollama", model: "qwen3.5:9b"}

	out := renderSidebar(s, "auto", nil, 30, 40)
	for _, want := range []string{"routing", "auto", "pin model", "pin provider"} {
		if !strings.Contains(out, want) {
			t.Fatalf("auto sidebar missing %q:\n%s", want, out)
		}
	}

	out = renderSidebar(s, "pinned", nil, 30, 40)
	if !strings.Contains(out, "routing") || !strings.Contains(out, "pinned") {
		t.Fatalf("pinned sidebar missing routing line:\n%s", out)
	}
	if strings.Contains(out, "pin model") {
		t.Fatalf("pinned sidebar must keep today's model label:\n%s", out)
	}
}

// The brain section renders only from a real report: local vs cloud placement
// plus the answering model id; nil report → no section at all (ADR-0125).
func TestRenderSidebarBrainSection(t *testing.T) {
	s := sidebarState{connected: true}

	out := renderSidebar(s, "auto", &BrainReport{Placement: "home", ModelID: "qwen3.5:9b"}, 30, 40)
	for _, want := range []string{"Brain", "local", "qwen3.5:9b", "routine"} {
		if !strings.Contains(out, want) {
			t.Fatalf("home brain section missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "cloud") {
		t.Fatalf("home brain section claims cloud:\n%s", out)
	}

	out = renderSidebar(s, "auto", &BrainReport{Placement: "cloud", ModelID: "gpt-x", Escalated: true}, 30, 40)
	for _, want := range []string{"Brain", "cloud", "gpt-x", "escalated"} {
		if !strings.Contains(out, want) {
			t.Fatalf("cloud brain section missing %q:\n%s", want, out)
		}
	}

	out = renderSidebar(s, "auto", nil, 30, 40)
	for _, banned := range []string{"Brain", "local", "cloud"} {
		if strings.Contains(out, banned) {
			t.Fatalf("nil brain report must render no section, found %q:\n%s", banned, out)
		}
	}
}

// Full-screen render, both modes: legacy output stays free of conductor
// artifacts and keeps today's title bar; auto output states the mode and the
// last answering brain without presenting the pin as the answerer.
func TestViewConductorDisplay(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	m.sidebar.connected = true
	m.sidebar.model = "qwen3.5:9b"
	m.sidebar.provider = "ollama"
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)

	out := m.View()
	if !strings.Contains(out, "Billy │ qwen3.5:9b │ ollama") {
		t.Fatalf("legacy view lost today's title bar:\n%s", out)
	}
	for _, banned := range []string{"routing", "local", "cloud", "Brain", "last:"} {
		if strings.Contains(out, banned) {
			t.Fatalf("legacy view leaks conductor artifact %q", banned)
		}
	}

	m.routingMode = "auto"
	m.lastBrain = &BrainReport{Placement: "cloud", ModelID: "gpt-x", Escalated: true}
	out = m.View()
	for _, want := range []string{"routing auto", "last: gpt-x", "cloud", "escalated"} {
		if !strings.Contains(out, want) {
			t.Fatalf("auto view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Billy │ qwen3.5:9b") {
		t.Fatalf("auto view still presents the pinned model as the answerer")
	}
}

// The help overlay documents the approval keys and the auto-mode meaning of
// the :model picker.
func TestHelpContentConductorLines(t *testing.T) {
	for _, want := range []string{"y / n", "Approve / decline", "sets the pin"} {
		if !strings.Contains(helpContent, want) {
			t.Fatalf("help overlay missing %q", want)
		}
	}
}
