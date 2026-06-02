package main

import "github.com/charmbracelet/lipgloss"

// ── palette ──────────────────────────────────────────────────────────────────

var (
	colorBillyResponse    = lipgloss.AdaptiveColor{Light: "#6B4C2A", Dark: "#F5E6D3"}
	colorUserInput        = lipgloss.AdaptiveColor{Light: "#0066CC", Dark: "#00CFFF"}
	colorBackground       = lipgloss.AdaptiveColor{Light: "#F0F0F0", Dark: "#1A1A1A"}
	colorStatusBar        = lipgloss.AdaptiveColor{Light: "#CCCCCC", Dark: "#2D2D2D"}
	colorGovernanceBorder = lipgloss.AdaptiveColor{Light: "#A07820", Dark: "#C5A243"}
	colorWarning          = lipgloss.AdaptiveColor{Light: "#B86000", Dark: "#FFB347"}
	colorError            = lipgloss.AdaptiveColor{Light: "#8B0000", Dark: "#C54E3A"}
	colorDiffAdd          = lipgloss.AdaptiveColor{Light: "#3A6B00", Dark: "#7CB32B"}
	colorDiffRemove       = lipgloss.AdaptiveColor{Light: "#8B0000", Dark: "#C54E3A"}
	colorText             = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#E8E8E8"}
	colorDim              = lipgloss.AdaptiveColor{Light: "#AAAAAA", Dark: "#555555"}
	colorSubtle           = lipgloss.AdaptiveColor{Light: "#888888", Dark: "#666666"}
	colorFocusBorder      = lipgloss.AdaptiveColor{Light: "#0066CC", Dark: "#00CFFF"}
	colorNormalBorder     = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#3A3A3A"}
	colorCodeBg           = lipgloss.AdaptiveColor{Light: "#E8E8E8", Dark: "#252525"}
	colorCommandBg        = lipgloss.AdaptiveColor{Light: "#E0E0FF", Dark: "#1E1E3A"}
	colorHelpBg           = lipgloss.AdaptiveColor{Light: "#F5F5F5", Dark: "#222222"}
	colorLatency          = lipgloss.AdaptiveColor{Light: "#3A6B00", Dark: "#7CB32B"}
	colorTokens           = lipgloss.AdaptiveColor{Light: "#6B4C2A", Dark: "#C5A243"}
)

// ── chat / response ───────────────────────────────────────────────────────────

var (
	BillyResponseStyle = lipgloss.NewStyle().
				Foreground(colorBillyResponse)

	UserInputStyle = lipgloss.NewStyle().
			Foreground(colorUserInput)

	ErrorStyle   = lipgloss.NewStyle().Foreground(colorError)
	WarningStyle = lipgloss.NewStyle().Foreground(colorWarning)
)

// ── status bar ────────────────────────────────────────────────────────────────

var (
	StatusBarStyle = lipgloss.NewStyle().
			Background(colorStatusBar).
			Foreground(colorText).
			Padding(0, 1)

	StatusLatencyStyle = lipgloss.NewStyle().
				Background(colorStatusBar).
				Foreground(colorLatency).
				Padding(0, 1)

	StatusTokenStyle = lipgloss.NewStyle().
				Background(colorStatusBar).
				Foreground(colorTokens).
				Padding(0, 1)
)

// ── pane borders ──────────────────────────────────────────────────────────────

var (
	FocusedPaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorFocusBorder)

	NormalPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorNormalBorder)
)

// ── governance alert ──────────────────────────────────────────────────────────

var GovernanceBorderStyle = lipgloss.NewStyle().
	BorderForeground(colorGovernanceBorder).
	Border(lipgloss.RoundedBorder())

// ── hint bar ─────────────────────────────────────────────────────────────────

var HintBarStyle = lipgloss.NewStyle().
	Foreground(colorSubtle).
	Background(colorBackground).
	Padding(0, 1)

// ── command bar ───────────────────────────────────────────────────────────────

var CommandBarStyle = lipgloss.NewStyle().
	Foreground(colorUserInput).
	Background(colorCommandBg).
	Padding(0, 1)

// ── help overlay ─────────────────────────────────────────────────────────────

var HelpOverlayStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colorText).
	Background(colorHelpBg).
	Foreground(colorText).
	Padding(1, 3)

// ── code block (streaming highlight, Phase 3b) ───────────────────────────────

var CodeBlockStyle = lipgloss.NewStyle().
	Background(colorCodeBg).
	Foreground(colorText)

// ── sidebar ───────────────────────────────────────────────────────────────────

var SectionHeaderStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(colorText)

// suppress unused colour warnings for palette entries not yet wired to styles
var (
	_ = colorBackground
	_ = colorDiffAdd
	_ = colorDiffRemove
	_ = colorDim
)
