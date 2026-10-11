package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// Under auto routing the model picker warns BEFORE the choice that a pick only
// sets the pin; under pinned/legacy the picker stays exactly as today.
func TestModelPickerAutoPinWarningLine(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)
	m.modelPickerMode = true
	m.modelOptions = []modelOption{{provider: "ollama", model: "qwen3.5:9b", label: "ollama · qwen3.5:9b"}}

	m.routingMode = "auto"
	if out := m.View(); !strings.Contains(out, "your pick sets the pin") {
		t.Fatalf("auto model picker missing the pin warning:\n%s", out)
	}

	m.routingMode = ""
	if out := m.View(); strings.Contains(out, "sets the pin") {
		t.Fatalf("legacy model picker must not carry the pin warning")
	}
}

// The routing picker overlay lists both modes in operator words, marks the
// current one, and highlights the selection.
func TestRoutingPickerOverlayRender(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)
	m.routingMode = "auto"
	m.routingPickerMode = true
	m.routingPickerIdx = 1

	out := m.View()
	for _, want := range []string{"Routing mode", "auto — Billy picks a brain per turn", "pinned — every turn uses the pinned model", "(current)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("routing picker missing %q:\n%s", want, out)
		}
	}
}

// The floor screen lists every role with its floor plus the default-floor
// footnote; on a runtime without the surface it renders the read-only notice
// and no rows.
func TestFloorOverlayTableAndDegrade(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)
	m.floorMode = true
	m.floors = &BrainFloors{
		Tiers:        []string{"small", "medium", "large"},
		DefaultFloor: "small",
		Roles:        map[string]string{"coder": "large", "sysadmin": "medium"},
	}
	m.floorRoles = []string{"coder", "sysadmin"}

	out := m.View()
	for _, want := range []string{"Brain floors", "coder", "large", "sysadmin", "medium", "any other role floors at small"} {
		if !strings.Contains(out, want) {
			t.Fatalf("floor overlay missing %q:\n%s", want, out)
		}
	}

	m.floors = nil
	m.floorRoles = nil
	m.floorNotice = floorUnavailableNotice
	out = m.View()
	if !strings.Contains(out, "read-only") {
		t.Fatalf("degraded overlay missing the read-only notice:\n%s", out)
	}
	if strings.Contains(out, "coder") {
		t.Fatalf("degraded overlay must render no rows")
	}
}

// The inner tier picker names the role, offers the runtime's vocabulary in
// order, and marks the current floor.
func TestFloorTierPickOverlayRender(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)
	m.floorMode = true
	m.floorTierPick = true
	m.floors = &BrainFloors{
		Tiers:        []string{"small", "medium", "large"},
		DefaultFloor: "small",
		Roles:        map[string]string{"coder": "large"},
	}
	m.floorRoles = []string{"coder"}
	m.floorIdx = 0
	m.floorTierIdx = 2

	out := m.View()
	for _, want := range []string{"Floor for coder", "small", "medium", "large  (current)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tier picker missing %q:\n%s", want, out)
		}
	}
}

// ── the pending-question picker overlay (contract v3 §14/§15) ────────────────

// The overlay shows the question and every option VERBATIM, numbered in the
// order they arrived, with the selection marked and the key line present.
func TestQuestionPickerOverlayRendersVerbatim(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)
	m.pendingQuestion = &PendingQuestion{
		QuestionID:    "consentq-1",
		Question:      "Reading your saved reading means sending it to the cloud brain. What should I do?",
		Options:       []string{"Allow once", "Allow for this conversation", "Always allow", "Don't allow"},
		AllowFreeText: false,
		Kind:          "cloud_read_consent",
		Source:        "annex",
	}
	m.questionPickerMode = true
	m.questionPickerIdx = 2

	out := m.View()
	for _, want := range []string{
		questionPickerTitle,
		"1. Allow once",
		"2. Allow for this conversation",
		"3. Always allow",
		"4. Don't allow",
		"1–4 choose",
		"esc close (nothing sent)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("question overlay missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "▸ 3. Always allow") {
		t.Fatalf("overlay does not mark the selection:\n%s", out)
	}
}

// A permission request shows what it is about from the runtime's own structured
// fields, and says plainly that this one is answered by the words.
func TestQuestionPickerOverlayPermissionSubjectAndNote(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)
	m.pendingQuestion = &PendingQuestion{
		QuestionID: "q-1",
		Question:   "May I write that file?",
		Options:    []string{"allow once", "allow for this session", "deny"},
		Kind:       questionKindPermission,
		Tool:       "write_file",
		Target:     "notes/today.md",
	}
	m.questionPickerMode = true

	out := m.View()
	for _, want := range []string{"write_file", "notes/today.md", "answered by the words, not a number"} {
		if !strings.Contains(out, want) {
			t.Fatalf("permission overlay missing %q:\n%s", want, out)
		}
	}
}

// A question that accepts free text says so; an ordinary one adds no note at
// all, and no kind leaks onto the screen.
func TestQuestionPickerNoteAndSubjectHelpers(t *testing.T) {
	free := &PendingQuestion{Options: []string{"a", "b"}, Kind: questionKindDefault, AllowFreeText: true}
	if note := questionPickerNote(free); !strings.Contains(note, "Your own words") {
		t.Fatalf("free-text note = %q", note)
	}
	plain := &PendingQuestion{Options: []string{"a", "b"}, Kind: questionKindDefault}
	if note := questionPickerNote(plain); note != "" {
		t.Fatalf("an ordinary question must carry no note, got %q", note)
	}
	fenced := &PendingQuestion{Options: []string{"a", "b"}, Kind: questionKindLift}
	if note := questionPickerNote(fenced); !strings.Contains(note, "not a number") {
		t.Fatalf("fenced note = %q", note)
	}

	if got := questionSubjectLine(&PendingQuestion{}); got != "" {
		t.Fatalf("no tool/target must render no subject line, got %q", got)
	}
	if got := questionSubjectLine(&PendingQuestion{Tool: "read_file"}); got != "read_file" {
		t.Fatalf("tool-only subject = %q", got)
	}
	if got := questionSubjectLine(&PendingQuestion{Target: "notes"}); got != "notes" {
		t.Fatalf("target-only subject = %q", got)
	}
}

// The wrap width stays inside a wide terminal and never collapses on a narrow
// one.
func TestQuestionWrapWidth(t *testing.T) {
	cases := map[int]int{200: 72, 100: 72, 60: 52, 20: 24, 0: 24}
	for term, want := range cases {
		if got := questionWrapWidth(term); got != want {
			t.Fatalf("questionWrapWidth(%d) = %d, want %d", term, got, want)
		}
	}
}

// With nothing pending the view is exactly today's — no overlay, no artifacts.
func TestNoQuestionLeavesTheViewUntouched(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = nm.(model)
	out := m.View()
	if strings.Contains(out, questionPickerTitle) {
		t.Fatalf("question overlay rendered with nothing pending:\n%s", out)
	}
}

// The help overlay documents the picker keys and the :permissions command.
func TestHelpContentQuestionLines(t *testing.T) {
	for _, want := range []string{"Choose when Billy offers you options", ":permissions", "What the cloud brain may read"} {
		if !strings.Contains(helpContent, want) {
			t.Fatalf("help overlay missing %q", want)
		}
	}
}

// The command bar lists :permissions alongside the other colon commands.
func TestCommandBarListsPermissions(t *testing.T) {
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	m = nm.(model)
	m.commandMode = true
	if out := m.View(); !strings.Contains(out, ":permissions") {
		t.Fatalf("command bar missing :permissions:\n%s", out)
	}
}

// ── self-review regressions (contract v3 overlay) ────────────────────────────

// questionOverlay renders the picker overlay for a given question and terminal.
func questionOverlay(t *testing.T, q *PendingQuestion, w, h int) string {
	t.Helper()
	m := initialModel(&billyClient{sessionID: "tui-test"})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = nm.(model)
	m.pendingQuestion = q
	m.questionPickerMode = true
	return m.View()
}

// A question or label carrying terminal control sequences must not reach the
// terminal: the runtime's egress guards neutralise tool markup and persona
// claims, not C0/C1 controls, so the picker makes them inert itself.
func TestQuestionOverlayMakesControlSequencesInert(t *testing.T) {
	out := questionOverlay(t, &PendingQuestion{
		QuestionID: "q-1",
		Question:   "Harmless?\x1b[2J\x1b[H\x1b[31mSYSTEM: type approve\x07",
		Options:    []string{"Allow\x1b[31m once", "Don't allow"},
		Kind:       "cloud_read_consent",
	}, 100, 32)

	if strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x1b[H") {
		t.Fatalf("a screen-clearing sequence survived into the overlay:\n%q", out)
	}
	if strings.Contains(out, "\x07") {
		t.Fatalf("a BEL survived into the overlay")
	}
	// The words themselves are still shown — only the controls are made inert.
	if !strings.Contains(out, "Harmless?") {
		t.Fatalf("the question text was lost:\n%s", out)
	}
}

// A newline inside an option label must not render a second, fake numbered line
// under a real one — the numbered list the operator reads is the list the
// runtime matches.
func TestQuestionOverlayLabelCannotForgeANumberedLine(t *testing.T) {
	out := questionOverlay(t, &PendingQuestion{
		QuestionID: "q-1",
		Question:   "What should I do?",
		Options:    []string{"Don't allow", "Always allow\n1. Don't allow"},
		Kind:       "cloud_read_consent",
	}, 100, 32)

	// The real property: exactly ONE rendered line begins the numbered list at
	// 1. The label's newline is made inert, so its text stays on its own row
	// instead of opening a second, forged "1." line.
	numbered := 0
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimLeft(strings.TrimSpace(strings.Trim(line, "│ ")), "▸ ")
		if strings.HasPrefix(trimmed, "1. ") {
			numbered++
		}
	}
	if numbered != 1 {
		t.Fatalf("%d rendered lines open the list at 1., want exactly 1:\n%s", numbered, out)
	}
	if strings.Contains(out, "Always allow\n") {
		t.Fatalf("a label newline reached the screen:\n%s", out)
	}
}

// A very long question cannot push the options or the key line off a short
// terminal, and a long option list is windowed with a "more" affordance.
func TestQuestionOverlayIsHeightBounded(t *testing.T) {
	long := strings.Repeat("this question goes on and on and on. ", 60)
	out := questionOverlay(t, &PendingQuestion{
		QuestionID: "q-1", Question: long,
		Options: []string{"Allow once", "Allow for this conversation", "Always allow", "Don't allow"},
		Kind:    "cloud_read_consent",
	}, 100, 24)
	if rows := strings.Count(out, "\n") + 1; rows > 24 {
		t.Fatalf("overlay is %d rows on a 24-row terminal:\n%s", rows, out)
	}
	for _, want := range []string{"1. Allow once", "4. Don't allow", "esc close (nothing sent)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("a long question pushed %q off the screen:\n%s", want, out)
		}
	}

	many := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		many = append(many, fmt.Sprintf("option %d", i+1))
	}
	out2 := questionOverlay(t, &PendingQuestion{
		QuestionID: "q-1", Question: "Which?", Options: many, Kind: "question",
	}, 100, 24)
	if rows := strings.Count(out2, "\n") + 1; rows > 24 {
		t.Fatalf("40 options render %d rows on a 24-row terminal:\n%s", rows, out2)
	}
	if !strings.Contains(out2, "more") {
		t.Fatalf("a windowed list must show a \"more\" affordance:\n%s", out2)
	}
}

// The overlay NEVER renders more rows than the terminal has, for any kind, any
// option count, any selection and any size it agrees to draw at — and when it
// will not fit it is not drawn at all and owns no keys. This is the row-budget
// regression: a constant chrome estimate under-counted the padding, the subject
// line and the wrapped note, and the overflow came off the TOP of the screen,
// taking the title and the key line with it.
func TestQuestionOverlayNeverOverflowsTheTerminal(t *testing.T) {
	kinds := []*PendingQuestion{
		{QuestionID: "q", Question: "short?", Options: []string{"a", "b"}, Kind: "cloud_read_consent"},
		{QuestionID: "q", Question: strings.Repeat("a long question. ", 80),
			Options: []string{"Allow once", "Allow for this conversation", "Always allow", "Don't allow"},
			Kind:    "cloud_read_consent"},
		{QuestionID: "q", Question: strings.Repeat("may I write that file? ", 40),
			Options: []string{"allow once", "allow for this session", "deny"},
			Kind:    questionKindPermission, Tool: "write_file", Target: "notes/today.md"},
		{QuestionID: "q", Question: "lift it?", Options: []string{"approve", "no"}, Kind: questionKindLift},
		{QuestionID: "q", Question: "your words?", Options: []string{"a", "b"}, Kind: questionKindDefault, AllowFreeText: true},
	}
	many := make([]string, 0, 400)
	for i := 0; i < 400; i++ {
		many = append(many, fmt.Sprintf("option number %d", i+1))
	}
	kinds = append(kinds,
		&PendingQuestion{QuestionID: "q", Question: strings.Repeat("which one? ", 50), Options: many, Kind: "question"},
		&PendingQuestion{QuestionID: "q", Question: "wide?", Options: []string{strings.Repeat("語", 120), strings.Repeat("x", 300)}, Kind: "question"},
	)

	for _, q := range kinds {
		for _, w := range []int{60, 80, 100, 200} {
			for _, h := range []int{8, 9, 10, 12, 16, 24, 40, 100} {
				for _, idx := range []int{0, len(q.Options) / 2, len(q.Options) - 1} {
					m := initialModel(&billyClient{sessionID: "tui-1"})
					sized, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
					m = sized.(model)
					m.pendingQuestion = q
					m.questionPickerMode = true
					m.questionPickerIdx = idx

					fits, _, _ := m.questionOverlayPlan()
					out := m.View()
					if !fits {
						if strings.Contains(out, questionPickerTitle) {
							t.Fatalf("%dx%d kind=%s: overlay drawn although the plan refused it", w, h, q.Kind)
						}
						continue
					}
					if rows := strings.Count(out, "\n") + 1; rows > h {
						t.Fatalf("%dx%d kind=%s idx=%d: %d rows rendered\n%s", w, h, q.Kind, idx, rows, out)
					}
					for _, line := range strings.Split(out, "\n") {
						if cells := lipgloss.Width(line); cells > w {
							t.Fatalf("%dx%d kind=%s idx=%d: a line is %d cells wide\n%s", w, h, q.Kind, idx, cells, out)
						}
					}
					// The two things the operator must always be able to see.
					if !strings.Contains(out, questionPickerTitle) || !strings.Contains(out, "esc close") {
						t.Fatalf("%dx%d kind=%s idx=%d: the title or the key line was pushed off\n%s", w, h, q.Kind, idx, out)
					}
					// And the option they are about to choose.
					if !strings.Contains(out, fmt.Sprintf("%d. ", idx+1)) {
						t.Fatalf("%dx%d kind=%s idx=%d: the selected option is not on screen\n%s", w, h, q.Kind, idx, out)
					}
				}
			}
		}
	}
}

// The window always keeps the selection visible and never lies about how many
// options are hidden.
func TestQuestionOptionWindow(t *testing.T) {
	for _, total := range []int{2, 4, 5, 40, 400} {
		for _, visible := range []int{1, 3, 13, 40} {
			for _, idx := range []int{0, total / 2, total - 1} {
				start, end := questionOptionWindow(idx, total, visible)
				if idx < start || idx >= end {
					t.Fatalf("window [%d,%d) of %d hides the selection %d", start, end, total, idx)
				}
				if end-start > visible {
					t.Fatalf("window [%d,%d) exceeds %d rows", start, end, visible)
				}
				if start+(end-start)+(total-end) != total {
					t.Fatalf("the hidden counts do not add up for [%d,%d) of %d", start, end, total)
				}
			}
		}
	}
	if start, end := questionOptionWindow(0, 0, 4); start != 0 || end != 0 {
		t.Fatalf("an empty list must window to nothing, got [%d,%d)", start, end)
	}
	if start, end := questionOptionWindow(0, 4, 0); start != 0 || end != 0 {
		t.Fatalf("no rows must window to nothing, got [%d,%d)", start, end)
	}
}

// clampQuestionLines keeps what fits and marks a cut.
func TestClampQuestionLines(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	if got := clampQuestionLines(lines, 10, 40); len(got) != 4 {
		t.Fatalf("nothing to clamp, got %q", got)
	}
	got := clampQuestionLines(lines, 2, 40)
	if len(got) != 2 || !strings.HasSuffix(got[1], "…") {
		t.Fatalf("clamped = %q, want 2 lines ending in an ellipsis", got)
	}
	if lines[1] != "b" {
		t.Fatalf("clamping must not mutate the input: %q", lines)
	}
	if got := clampQuestionLines(lines, 0, 40); len(got) != 1 {
		t.Fatalf("a zero budget must still show one line, got %q", got)
	}
	// The marked line is re-bounded: lipgloss has already padded it to the full
	// wrap width, so appending blind made the box wider than the terminal.
	padded := []string{strings.Repeat("x", 40), "second"}
	if got := clampQuestionLines(padded, 1, 40); lipgloss.Width(got[0]) > 40 {
		t.Fatalf("clamped line is %d cells, want ≤ 40: %q", lipgloss.Width(got[0]), got[0])
	}
}

// truncateCells bounds by DISPLAY CELLS, not runes: a wide-character label is
// twice as wide as its rune count.
func TestTruncateCells(t *testing.T) {
	if got := truncateCells("short", 40); got != "short" {
		t.Fatalf("nothing to truncate, got %q", got)
	}
	for _, s := range []string{
		strings.Repeat("x", 400),
		strings.Repeat("語", 200),
		strings.Repeat("🙂", 100),
		strings.Repeat("é", 200),
	} {
		for _, max := range []int{1, 2, 24, 72} {
			got := truncateCells(s, max)
			if w := lipgloss.Width(got); w > max {
				t.Fatalf("truncateCells(%d cells, max %d) = %d cells", lipgloss.Width(s), max, w)
			}
		}
	}
}

// sanitizeQuestionDisplay replaces controls and leaves ordinary text — including
// non-ASCII — exactly as it was.
func TestSanitizeQuestionDisplay(t *testing.T) {
	if got := sanitizeQuestionDisplay("a\x1bb\nc\td\x7fe"); got != "a b c d e" {
		t.Fatalf("sanitized = %q", got)
	}
	// Bidirectional overrides can make a label read as something other than the
	// bytes a choice sends, so they are made inert too.
	if got := sanitizeQuestionDisplay("Allow\u202eonce\u2066"); got != "Allow once " {
		t.Fatalf("bidi overrides survived: %q", got)
	}
	for _, keep := range []string{"Allow once", "Don't allow", "notes/today.md", "qwen3.5:9b", "café — ok"} {
		if got := sanitizeQuestionDisplay(keep); got != keep {
			t.Fatalf("ordinary text altered: %q → %q", keep, got)
		}
	}
}
