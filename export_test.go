package main

import (
	"os"
	"strings"
	"testing"
)

// govNotice is the TUI-generated governance line. It deliberately carries no
// "[Billy]" prefix so the export/save paths never attribute it to Billy.
const govNotice = "🛡 Action blocked by governance policy."

// A TUI governance notice must not be attributed to Billy in a conversation
// export. Unprefixed, it is neither a [You] nor a [Billy] turn, so exportChat
// (which emits only real conversation) omits it, while the real Billy reply
// still appears. Regression for the old "[Billy] 🛡 …" prefix.
func TestExportChatOmitsUnattributedGovernanceNotice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	messages := []string{"[You] hi", "[Billy] hello there", govNotice}

	path, err := exportChat(messages, "sess-1")
	if err != nil {
		t.Fatalf("exportChat: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	out := string(data)
	if strings.Contains(out, "Action blocked by governance policy") {
		t.Fatalf("governance notice leaked into export:\n%s", out)
	}
	if !strings.Contains(out, "hello there") {
		t.Fatalf("real Billy reply missing from export:\n%s", out)
	}
}

// The ctrl+s debug save (buildMarkdown) must render the unattributed notice as
// a system note, never under a "**Billy:**" heading.
func TestBuildMarkdownDoesNotLabelNoticeAsBilly(t *testing.T) {
	out := buildMarkdown([]string{"[Billy] real reply", govNotice}, "sess-1")
	if strings.Contains(out, "**Billy:** 🛡") || strings.Contains(out, "**Billy:** Action blocked") {
		t.Fatalf("notice attributed to Billy in debug save:\n%s", out)
	}
	if !strings.Contains(out, "**Billy:** real reply") {
		t.Fatalf("real Billy reply missing or mislabeled:\n%s", out)
	}
}
