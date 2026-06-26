package main

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
)

type pane int

const (
	paneInput pane = iota
	paneChat
)

type model struct {
	// layout
	width        int
	height       int
	focusedPane  pane
	chatViewport viewport.Model
	sidebarWidth int
	ready        bool

	// chat content
	messages        []string
	displayMessages []string
	liveMsg         string

	// input — textarea supports programmatic SetValue() for future voice injection
	input      textarea.Model
	draftInput string // saved when navigating history
	lastPrompt string // last submitted prompt, restored to the input if the turn fails

	// input history ring buffer (max 50)
	inputHistory []string
	historyIdx   int // -1 = not navigating; ≥0 = index into history (0=newest)

	// command palette
	commandMode  bool
	commandInput textinput.Model

	// UI overlays
	showHelp bool

	// model picker (':model' command)
	modelPickerMode bool
	modelOptions    []modelOption
	modelPickerIdx  int

	// spinner / response state
	spinner      spinner.Model
	thinking     bool
	isStreaming  bool
	streamBuffer string
	streamTokens int

	// in-flight turn control. streamGen tags every turn; messages from a turn
	// whose Gen != streamGen are stale (a newer turn started, or this one was
	// cancelled) and are ignored. streamCancel cancels the in-flight request's
	// context (esc / abandon, or release on completion).
	streamGen    int
	streamCancel context.CancelFunc

	// timing
	requestStarted time.Time
	lastLatency    time.Duration

	// runtime
	client               *billyClient
	sidebar              sidebarState
	governanceAlertTicks int
	lastGovRejected      int

	// notifications
	saveStatus      string
	saveStatusTicks int

	// markdown renderer — created once, recreated on resize (Phase 3b)
	mdRenderer *glamour.TermRenderer
}

// --- message types ---

type responseMsg struct {
	text string
	Gen  int
}
type errMsg struct {
	text string
	Gen  int
}

// turnInProgressMsg signals that billy-runtime returned HTTP 409
// (session_turn_in_progress): Billy is still working on the previous turn.
// It is surfaced as a transient, non-alarming status, never as an error.
type turnInProgressMsg struct{ Gen int }

type healthResultMsg struct{ err error }

// modelOption is one selectable provider/model entry in the ':model' picker.
type modelOption struct {
	provider string
	model    string
	label    string
}

type modelsLoadedMsg struct {
	options []modelOption
	err     error
}
type modelSetMsg struct {
	provider string
	model    string
	err      error
}

type StreamChunkMsg struct {
	Chunk  string
	Prompt string
	Gen    int
	events <-chan streamEvent
}
type StreamDoneMsg struct {
	FullText string
	Gen      int
}
type StreamErrMsg struct {
	Prompt string
	Err    error
	Gen    int
}

// --- constructors ---

func newMdRenderer(width int) *glamour.TermRenderer {
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	return r
}

func initialModel(client *billyClient) model {
	ti := textarea.New()
	ti.Placeholder = "Message Billy…   ↑/↓ history   ? help   :command"
	ti.ShowLineNumbers = false
	ti.CharLimit = 4096
	ti.SetHeight(1)
	ti.Focus()

	ci := textinput.New()
	ci.Placeholder = "type a command…"
	ci.Prompt = ""
	ci.CharLimit = 256

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	return model{
		// Start with an empty transcript. The TUI must never speak AS Billy:
		// the first "[Billy]" line in the transcript is Billy's real response
		// from the runtime. `messages` (used for export / debug-save) stays
		// empty so persisted transcripts contain only real conversation.
		messages: []string{},
		// `displayMessages` carries a single dim, non-attributed UI hint so the
		// chat pane is not blank at startup. It is clearly the interface, not
		// Billy (DimStyle, no "[Billy]" prefix), and is overwritten as soon as
		// the first real message arrives.
		displayMessages: []string{
			DimStyle.Render("— Say hi to start. —"),
		},
		input:        ti,
		commandInput: ci,
		spinner:      sp,
		client:       client,
		historyIdx:   -1,
		focusedPane:  paneInput,
		mdRenderer:   newMdRenderer(80),
	}
}
