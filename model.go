package main

import (
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
	voiceReady bool   // stub: true when audio device is available (Phase 4)

	// input history ring buffer (max 50)
	inputHistory []string
	historyIdx   int // -1 = not navigating; ≥0 = index into history (0=newest)

	// command palette
	commandMode  bool
	commandInput textinput.Model

	// UI overlays
	showHelp bool

	// spinner / response state
	spinner      spinner.Model
	thinking     bool
	isStreaming  bool
	streamBuffer string
	streamTokens int

	// timing
	requestStarted time.Time
	lastLatency    time.Duration

	// runtime
	client               *billyClient
	sidebar              sidebarState
	governanceAlertTicks int
	lastGovernanceEvent  string

	// notifications
	saveStatus      string
	saveStatusTicks int

	// markdown renderer — created once, recreated on resize (Phase 3b)
	mdRenderer *glamour.TermRenderer
}

// --- message types ---

type responseMsg struct{ text string }
type errMsg struct{ text string }
type healthResultMsg struct{ err error }

type StreamChunkMsg struct {
	Chunk  string
	Prompt string
	events <-chan streamEvent
}
type StreamDoneMsg struct{ FullText string }
type StreamErrMsg struct {
	Prompt string
	Err    error
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
		messages: []string{
			"[Billy] Hey. What can I build for you?",
		},
		displayMessages: []string{
			BillyResponseStyle.Render("[Billy] ") + "Hey. What can I build for you?",
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
