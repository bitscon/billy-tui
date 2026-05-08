package main

import "github.com/charmbracelet/lipgloss"

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

	StatusBarStyle = lipgloss.NewStyle().
			Background(colorStatusBar).
			Foreground(colorText).
			Padding(0, 1)

	BillyResponseStyle = lipgloss.NewStyle().
				Foreground(colorBillyResponse)

	UserInputStyle = lipgloss.NewStyle().
			Foreground(colorUserInput)

	SectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorText)

	GovernanceBorderStyle = lipgloss.NewStyle().
				BorderForeground(colorGovernanceBorder).
				Border(lipgloss.RoundedBorder())

	WarningStyle = lipgloss.NewStyle().Foreground(colorWarning)
	ErrorStyle   = lipgloss.NewStyle().Foreground(colorError)

	// These variables are defined to satisfy the full ADR-0125 palette requirement.
	_ = colorBackground
	_ = colorDiffAdd
	_ = colorDiffRemove
)
