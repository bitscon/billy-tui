package main

import (
	"strings"
	"testing"
)

// TestFormatBrainBadge covers the contract's flag-precedence ladder (§3):
// failsafe beats every other flag, the privacy pair beats escalation, and a
// report with no flags reads as routine. Nil renders as "".
func TestFormatBrainBadge(t *testing.T) {
	cases := []struct {
		name string
		b    *BrainReport
		want string
	}{
		{"nil report", nil, ""},
		{
			"routine home",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b"},
			"local · qwen3.5:9b · routine",
		},
		{
			"escalated cloud",
			&BrainReport{Placement: "cloud", ModelID: "big-model", Escalated: true},
			"cloud · big-model · escalated",
		},
		{
			"failsafe beats escalated",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b", Failsafe: true, Escalated: true},
			"local · qwen3.5:9b · fail-safe (kept home)",
		},
		{
			"degraded_for_privacy → private label",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b", DegradedForPrivacy: true, Escalated: true},
			"local · qwen3.5:9b · kept home (private)",
		},
		{
			"pinned_home → private label",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b", PinnedHome: true},
			"local · qwen3.5:9b · kept home (private)",
		},
		{
			"failsafe beats privacy flags",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b", Failsafe: true, DegradedForPrivacy: true, PinnedHome: true},
			"local · qwen3.5:9b · fail-safe (kept home)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatBrainBadge(tc.b); got != tc.want {
				t.Fatalf("formatBrainBadge = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBrainRecordLine verifies the transcript record: prefix + badge + reason,
// with the separator omitted on an empty reason and newlines flattened so the
// line stays single-line in exports and debug captures.
func TestBrainRecordLine(t *testing.T) {
	cases := []struct {
		name string
		b    *BrainReport
		want string
	}{
		{"nil report", nil, ""},
		{
			"badge with reason",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b", Reason: "routine turn; resolved at home"},
			"[Brain] local · qwen3.5:9b · routine — routine turn; resolved at home",
		},
		{
			"empty reason omits separator",
			&BrainReport{Placement: "cloud", ModelID: "big-model", Escalated: true},
			"[Brain] cloud · big-model · escalated",
		},
		{
			"newlines in reason flattened",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b", Reason: "line one\nline two\r\nline three"},
			"[Brain] local · qwen3.5:9b · routine — line one line two line three",
		},
		{
			"whitespace-only reason omits separator",
			&BrainReport{Placement: "home", ModelID: "qwen3.5:9b", Reason: "\n\r\n"},
			"[Brain] local · qwen3.5:9b · routine",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := brainRecordLine(tc.b)
			if got != tc.want {
				t.Fatalf("brainRecordLine = %q, want %q", got, tc.want)
			}
			if strings.ContainsAny(got, "\n\r") {
				t.Fatalf("brainRecordLine must be single-line, got %q", got)
			}
		})
	}
}
