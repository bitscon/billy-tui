package main

import (
	"os"
	"path/filepath"
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

// homeHasChatFiles reports whether any billy-chat-*.md landed in the $HOME root
// — the clutter the tidy-up removes.
func homeHasChatFiles(t *testing.T, home string) bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, "billy-chat-*.md"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return len(matches) > 0
}

// :export must default under ~/.billy/exports and leave the $HOME root clean —
// no billy-chat-DATE.md dumped into home anymore.
func TestExportChatWritesUnderBillyDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BILLY_EXPORT_DIR", "") // force the default path

	path, err := exportChat([]string{"[You] hi", "[Billy] hey"}, "sess-1")
	if err != nil {
		t.Fatalf("exportChat: %v", err)
	}
	wantDir := filepath.Join(home, ".billy", "exports")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("export dir = %s, want %s", filepath.Dir(path), wantDir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("export file missing: %v", err)
	}
	if homeHasChatFiles(t, home) {
		t.Fatalf("export leaked a file into the $HOME root")
	}
}

// A BILLY_EXPORT_DIR override is honored verbatim.
func TestExportChatHonorsEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	override := t.TempDir()
	t.Setenv("BILLY_EXPORT_DIR", override)

	path, err := exportChat([]string{"[You] hi", "[Billy] hey"}, "sess-1")
	if err != nil {
		t.Fatalf("exportChat: %v", err)
	}
	if filepath.Dir(path) != override {
		t.Fatalf("export dir = %s, want override %s", filepath.Dir(path), override)
	}
}

// ctrl+s save must put both the "latest" pointer and the timestamped archive
// under ~/.billy/debug — nothing in the $HOME root.
func TestSaveChatWritesUnderBillyDebug(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BILLY_DEBUG_DIR", "") // force the default path

	latest, err := saveChat([]string{"[You] hi", "[Billy] hey"}, "sess-1")
	if err != nil {
		t.Fatalf("saveChat: %v", err)
	}
	debugDir := filepath.Join(home, ".billy", "debug")
	if filepath.Dir(latest) != debugDir {
		t.Fatalf("latest dir = %s, want %s", filepath.Dir(latest), debugDir)
	}
	if filepath.Base(latest) != "billy-chat-debug-latest.md" {
		t.Fatalf("latest name = %s", filepath.Base(latest))
	}
	// A timestamped archive should sit alongside the latest pointer.
	archives, err := filepath.Glob(filepath.Join(debugDir, "billy-chat-2*.md"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(archives) == 0 {
		t.Fatalf("no archive written under %s", debugDir)
	}
	if homeHasChatFiles(t, home) {
		t.Fatalf("save leaked a file into the $HOME root")
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
