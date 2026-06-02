package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type sidebarState struct {
	// runtime info (polled every 5s)
	runtimeStatus string
	model         string
	mode          string

	// telemetry
	recentEvents []string

	// governance
	governanceState string

	// observation (updated when Billy observes something)
	lastObservationTool   string
	lastObservationStatus string
	lastObservationReceipt string

	// memory context (from runtime if available)
	memoryHints []string
}

func sectionHeader(icon, title string) string {
	return SectionHeaderStyle.Render(icon+" "+title)
}

func renderSidebar(s sidebarState, width, height int) string {
	style := lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1)

	var sections []string

	// ── Runtime ──────────────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("⚡", "Runtime"))
		if s.model != "" {
			lines = append(lines, "  model: "+truncate(s.model, width-10))
		}
		if s.mode != "" {
			lines = append(lines, "  mode:  "+s.mode)
		}
		if s.runtimeStatus == "" {
			lines = append(lines, "  "+DimStyle.Render("connecting…"))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Governance ────────────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("🛡️", "Governance"))
		state := s.governanceState
		if state == "" {
			state = "active"
		}
		lines = append(lines, "  "+state)
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Tools / Telemetry ─────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("⚙️", "Tools"))
		if len(s.recentEvents) == 0 {
			lines = append(lines, "  "+DimStyle.Render("idle"))
		} else {
			for _, e := range s.recentEvents {
				lines = append(lines, "  • "+truncate(e, width-5))
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Observation ───────────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("👁️", "Observation"))
		if s.lastObservationTool == "" {
			lines = append(lines, "  "+DimStyle.Render("no observations yet"))
		} else {
			lines = append(lines, "  tool:   "+s.lastObservationTool)
			lines = append(lines, "  status: "+s.lastObservationStatus)
			if s.lastObservationReceipt != "" {
				rcpt := s.lastObservationReceipt
				if len(rcpt) > 12 {
					rcpt = rcpt[:12] + "…"
				}
				lines = append(lines, "  rcpt:   "+rcpt)
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Memory ────────────────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("🧠", "Memory"))
		if len(s.memoryHints) == 0 {
			lines = append(lines, "  "+DimStyle.Render("no context"))
		} else {
			for _, h := range s.memoryHints {
				lines = append(lines, "  • "+truncate(h, width-5))
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	content := strings.Join(sections, "\n\n")
	return style.Render(content)
}

func truncate(s string, max int) string {
	if max < 1 {
		return s
	}
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
