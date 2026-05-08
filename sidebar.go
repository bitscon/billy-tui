package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type sidebarState struct {
	runtimeStatus   string
	recentEvents    []string
	governanceState string
}

func renderSidebar(s sidebarState, width, height int) string {
	style := lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1)

	memory := SectionHeaderStyle.Render("🧠 Memory") + "\n(none yet)\n"

	tools := SectionHeaderStyle.Render("⚙️ Tools") + "\n"
	if len(s.recentEvents) == 0 {
		tools += "(idle)\n"
	} else {
		for _, e := range s.recentEvents {
			tools += "• " + e + "\n"
		}
	}

	gov := SectionHeaderStyle.Render("🛡️ Governance") + "\n"
	if s.governanceState == "" {
		gov += "auto-approve\n"
	} else {
		gov += s.governanceState + "\n"
	}

	content := strings.Join([]string{memory, tools, gov}, "\n")
	_ = fmt.Sprintf // suppress unused import if needed
	return style.Render(content)
}
