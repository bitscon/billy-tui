package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// sidebarState holds operator-facing live state. Every field is sourced from a
// real billy-runtime endpoint or a real local measurement — no placeholders.
// (ADR-0125 Amendment 2026-06-20.)
type sidebarState struct {
	// connection + llm (GET /health, GET /api/v1/llm/config)
	connected bool
	sessionID string
	provider  string
	model     string

	// runtime (GET /runtime/status)
	runtimeOK        bool
	lifecycleState   string
	runtimePhase     int
	currentStage     string
	targetStage      string
	executionEnabled bool

	// latency (computed locally, mirrored from the model on each response)
	lastLatency string // pre-formatted, e.g. "1.4s"
	lastTokens  int

	// governance (GET /runtime/status.telemetry + GET /reconciliation/recent)
	govReady       bool
	govAllowed     int
	govRejected    int
	lastDenialCode string

	// live work progress (GET /api/v1/execution/jobs/active)
	jobsReady  bool
	activeJobs []ActiveJob
}

func sectionHeader(icon, title string) string {
	return SectionHeaderStyle.Render(icon + " " + title)
}

// kv renders an indented "key value" line with a dim key.
func kv(key, val string) string {
	return "  " + SidebarKeyStyle.Render(key) + " " + val
}

func renderSidebar(s sidebarState, width, height int) string {
	style := lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1)

	var sections []string

	// ── Connection + LLM ──────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("◈", "Connection"))
		if s.connected {
			lines = append(lines, "  "+SidebarOKStyle.Render("● online"))
		} else {
			lines = append(lines, "  "+SidebarBadStyle.Render("● offline"))
		}
		if s.sessionID != "" {
			lines = append(lines, kv("session", truncate(s.sessionID, width-12)))
		}
		if s.model != "" {
			lines = append(lines, kv("model", truncate(s.model, width-10)))
		}
		if s.provider != "" {
			lines = append(lines, kv("provider", truncate(s.provider, width-12)))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Runtime ───────────────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("⚡", "Runtime"))
		if !s.runtimeOK {
			lines = append(lines, "  "+DimStyle.Render("unavailable"))
		} else {
			if s.lifecycleState != "" {
				lines = append(lines, kv("state", truncate(s.lifecycleState, width-10)))
			}
			lines = append(lines, kv("phase", fmt.Sprintf("%d", s.runtimePhase)))
			if s.currentStage != "" {
				stage := s.currentStage
				if s.targetStage != "" && s.targetStage != s.currentStage {
					stage += " → " + s.targetStage
				}
				lines = append(lines, kv("stage", truncate(stage, width-10)))
			}
			exec := SidebarBadStyle.Render("disabled")
			if s.executionEnabled {
				exec = SidebarOKStyle.Render("enabled")
			}
			lines = append(lines, kv("exec", exec))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Latency ───────────────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("⏱", "Latency"))
		if s.lastLatency == "" {
			lines = append(lines, "  "+DimStyle.Render("no requests yet"))
		} else {
			lines = append(lines, kv("last", s.lastLatency))
			if s.lastTokens > 0 {
				lines = append(lines, kv("tokens", fmt.Sprintf("~%d (est)", s.lastTokens)))
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Governance ────────────────────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("🛡", "Governance"))
		if !s.govReady {
			lines = append(lines, "  "+DimStyle.Render("unavailable"))
		} else {
			lines = append(lines, kv("allowed", SidebarOKStyle.Render(fmt.Sprintf("%d", s.govAllowed))))
			rej := fmt.Sprintf("%d", s.govRejected)
			if s.govRejected > 0 {
				rej = SidebarBadStyle.Render(rej)
			}
			lines = append(lines, kv("rejected", rej))
			if s.lastDenialCode != "" {
				lines = append(lines, kv("last deny", SidebarWarnStyle.Render(truncate(s.lastDenialCode, width-12))))
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	// ── Live Work Progress (I125) ─────────────────────────────────────────────
	{
		var lines []string
		lines = append(lines, sectionHeader("◷", "Work (I125)"))
		switch {
		case !s.jobsReady:
			lines = append(lines, "  "+DimStyle.Render("unavailable"))
		case len(s.activeJobs) == 0:
			lines = append(lines, "  "+DimStyle.Render("idle — no active jobs"))
		default:
			lines = append(lines, kv("active", fmt.Sprintf("%d", len(s.activeJobs))))
			shown := len(s.activeJobs)
			if shown > 3 {
				shown = 3
			}
			for _, j := range s.activeJobs[:shown] {
				label := j.CapabilityID
				if label == "" {
					label = j.ExecutionJobID
				}
				lines = append(lines, "  • "+SidebarKeyStyle.Render(j.CurrentState)+" "+truncate(label, width-12))
			}
			if len(s.activeJobs) > shown {
				lines = append(lines, "  "+DimStyle.Render(fmt.Sprintf("+%d more", len(s.activeJobs)-shown)))
			}
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	content := strings.Join(sections, "\n\n")
	return style.Render(content)
}

// truncate shortens s to at most max display runes, appending an ellipsis when
// it cuts. It counts and slices by rune, so it never splits a multi-byte
// character in a model name or denial code.
func truncate(s string, max int) string {
	if max < 1 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
