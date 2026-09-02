package main

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/key"
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

// maxInputRows caps how tall the multi-line input box may grow. The box starts
// at one row and grows by one for each newline (ctrl+j / alt+enter) the operator
// enters, up to this ceiling, after which the textarea scrolls internally.
const maxInputRows = 6

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

	// mouseCapture tracks whether the terminal mouse is captured. It starts false
	// so native text selection / copy works; ctrl+t toggles it. When on, the chat
	// viewport gains mouse-wheel scroll but native select-and-copy is suppressed.
	mouseCapture bool

	// model picker (':model' command)
	modelPickerMode bool
	modelOptions    []modelOption
	modelPickerIdx  int

	// routing-mode picker (':routing' command, Modernization 6)
	routingPickerMode bool
	routingPickerIdx  int

	// brain-floor screen (':brains' command, Modernization 5). floors stays nil
	// while loading and on a runtime without the surface — the screen is then
	// read-only and never writes (contract v2 §8 gate).
	floorMode     bool
	floors        *BrainFloors
	floorRoles    []string // stable sorted row order for floors.Roles
	floorIdx      int      // selected role row
	floorTierPick bool     // inner tier picker open for the selected role
	floorTierIdx  int
	floorNotice   string // in-overlay status: loading / unavailable / error / took-effect

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

	// conductor state (wire contract v1). routingMode mirrors the runtime's
	// routing_mode ("" = legacy runtime → today's pinned display). lastBrain is
	// the most recent per-answer routing report; pendingApproval is a reply
	// waiting on the operator's yes/no. Populated by the update handlers
	// (another lane); nil/empty until then and on every legacy runtime.
	routingMode     string
	lastBrain       *BrainReport
	pendingApproval *ApprovalRequest

	// queuedPrompt holds one prompt typed while a turn was still in flight
	// (enter-while-busy). It auto-sends when the turn completes cleanly; on any
	// failure path it is moved back into the input instead, so a reply typed
	// early — e.g. a fast "yes" to an approval — is never dropped silently.
	// At most one prompt queues; a newer enter-while-busy replaces it.
	queuedPrompt string

	// notifications
	saveStatus      string
	saveStatusTicks int

	// markdown renderer — created once, recreated on resize (Phase 3b)
	mdRenderer *glamour.TermRenderer
}

// --- conductor wire types (docs/CONDUCTOR-WIRE-CONTRACT.md §5, pinned) ---

// BrainReport mirrors the subset of the runtime's BrainSelection trace the
// client renders. Nil = no routing decision reported for this reply.
type BrainReport struct {
	Placement          string `json:"placement"` // "home" | "cloud"
	Provider           string `json:"provider"`
	ModelID            string `json:"model_id"`
	Reason             string `json:"reason"`
	Escalated          bool   `json:"escalated"`
	PinnedHome         bool   `json:"pinned_home"`
	DegradedForPrivacy bool   `json:"degraded_for_privacy"`
	Failsafe           bool   `json:"failsafe"`
	EffectiveTier      string `json:"effective_tier"`
}

// ApprovalRequest marks a reply that waits on the operator's yes/no.
// Nil = nothing pending.
type ApprovalRequest struct {
	Pending bool   `json:"pending"`
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Command string `json:"command"`
	Target  string `json:"target"`
}

// --- message types ---

type responseMsg struct {
	text     string
	Brain    *BrainReport     // nil on legacy runtimes / non-routed turns (contract §1)
	Approval *ApprovalRequest // nil = nothing awaits the operator
	Gen      int
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

// floorsLoadedMsg is the result of fetching the brain-floor table.
// unsupported marks a legacy runtime (404): the screen degrades read-only.
type floorsLoadedMsg struct {
	floors      *BrainFloors
	unsupported bool
	err         error
}

// floorSetMsg is the result of setting one role's floor. On success floors is
// the runtime's post-write table (contract v2 §10) — the took-effect proof.
type floorSetMsg struct {
	role        string
	tier        string
	floors      *BrainFloors
	unsupported bool
	err         error
}

// routingSetMsg is the result of a mode-only config POST (contract v2 §9). On
// success cfg carries the runtime's authoritative config reply — including the
// now-effective routing_mode — which the handler adopts; the 5s poll remains
// the standing authority afterwards.
type routingSetMsg struct {
	cfg *LLMConfig
	err error
}

type StreamChunkMsg struct {
	Chunk  string
	Prompt string
	Gen    int
	events <-chan streamEvent
}
type StreamDoneMsg struct {
	FullText string
	Brain    *BrainReport     // nil on legacy runtimes / non-routed turns (contract §1)
	Approval *ApprovalRequest // nil = nothing awaits the operator
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
	ti.Placeholder = "Message Billy…   ctrl+j newline   ↑/↓ history   ? help   :command"
	ti.ShowLineNumbers = false
	ti.CharLimit = 4096
	ti.Prompt = "> "
	// `enter` is intercepted for submit before the textarea sees it, so rebind
	// newline insertion to ctrl+j (reliably distinct from enter — it is LF, not
	// CR) and alt+enter, letting the operator write multi-line prompts.
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+j", "alt+enter"),
		key.WithHelp("ctrl+j", "newline"),
	)
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
