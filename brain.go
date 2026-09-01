package main

import "strings"

// brainLinePrefix marks a transcript line that records a turn's routing
// decision. Other lanes append these lines to the raw transcript, so they flow
// into exports and debug captures; keeping the prefix a single constant keeps
// those captures grep-able.
const brainLinePrefix = "[Brain] "

// formatBrainBadge renders a brain report as a short badge, e.g.
// "local · qwen3.5:9b · routine" or "cloud · gpt-x · escalated". Placement maps
// home → "local" (any other value passes through verbatim rather than being
// mislabeled). The short label follows the contract's flag precedence (§3):
// failsafe wins over everything, the two kept-home-for-privacy flags come next,
// then escalation, else routine. Nil-safe: no report renders as "".
func formatBrainBadge(b *BrainReport) string {
	if b == nil {
		return ""
	}
	placement := b.Placement
	if placement == "home" {
		placement = "local"
	}
	var label string
	switch {
	case b.Failsafe:
		label = "fail-safe (kept home)"
	case b.DegradedForPrivacy || b.PinnedHome:
		label = "kept home (private)"
	case b.Escalated:
		label = "escalated"
	default:
		label = "routine"
	}
	return placement + " · " + b.ModelID + " · " + label
}

// brainRecordLine renders the full transcript record for a brain report:
// prefix + badge + " — " + reason. The line lands in the raw transcript and
// debug captures, so it must stay deterministic and single-line — any newlines
// in Reason are flattened to spaces. When the (flattened) reason is empty the
// " — " separator is omitted. Nil-safe: no report renders as "".
func brainRecordLine(b *BrainReport) string {
	if b == nil {
		return ""
	}
	line := brainLinePrefix + formatBrainBadge(b)
	reason := strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(b.Reason))
	if reason != "" {
		line += " — " + reason
	}
	return line
}
