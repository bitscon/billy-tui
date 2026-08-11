package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// billyDir resolves a billy-tui output directory and creates it. It uses the
// value of envVar when set, otherwise ~/.billy/<sub...>. Keeping every artifact
// under ~/.billy (never the $HOME root) is deliberate — the home root stays
// clean and nothing lands where it could clutter or be committed by accident.
func billyDir(envVar string, sub ...string) (string, error) {
	dir := os.Getenv(envVar)
	if dir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home dir: %w", err)
		}
		dir = filepath.Join(append([]string{homeDir, ".billy"}, sub...)...)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", dir, err)
	}
	return dir, nil
}

// defaultDebugDir is where ctrl+s writes the chat debug log. It lives outside any
// user's $HOME on purpose: billy-tui runs as the operator, but the "debug billy"
// investigation runs under a separate agent account that must be able to read the
// capture. /tmp is world-readable and needs no root setup; a reboot clearing it is
// fine for a transient debug log. Override with BILLY_DEBUG_DIR (e.g. point it at
// /var/lib/billy/chat-log for a reboot-durable dir, once that exists group-writable).
const defaultDebugDir = "/tmp/billy/chat-log"

// saveChat writes the raw debug log — preserves all message types including
// errors and system notices. Used by ctrl+s.
//
// Both the stable "latest" pointer and the timestamped archive live in
// defaultDebugDir (override with BILLY_DEBUG_DIR); nothing is written to the
// $HOME root. The location sits outside $HOME so the agent account that runs the
// "debug billy" flow can read what the operator saves — cross-account reach is
// the whole reason it moved off the home directory.
func saveChat(messages []string, sessionID string) (string, error) {
	debugDir := os.Getenv("BILLY_DEBUG_DIR")
	if debugDir == "" {
		debugDir = defaultDebugDir
	}
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", debugDir, err)
	}

	latestPath := filepath.Join(debugDir, "billy-chat-debug-latest.md")
	timestamp := time.Now().Format("2006-01-02-150405")
	archivePath := filepath.Join(debugDir, "billy-chat-"+timestamp+".md")

	content := buildMarkdown(messages, sessionID)

	if err := os.WriteFile(latestPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("cannot write latest: %w", err)
	}
	if err := os.WriteFile(archivePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("cannot write archive: %w", err)
	}
	return latestPath, nil
}

// exportChat writes a clean, human-readable conversation export. Used by
// :export. Defaults to ~/.billy/exports (override with BILLY_EXPORT_DIR) so the
// export never clutters the $HOME root.
func exportChat(messages []string, sessionID string) (string, error) {
	exportDir, err := billyDir("BILLY_EXPORT_DIR", "exports")
	if err != nil {
		return "", err
	}

	timestamp := time.Now().Format("2006-01-02-150405")
	filename := fmt.Sprintf("billy-chat-%s.md", timestamp)
	path := filepath.Join(exportDir, filename)

	var b strings.Builder
	b.WriteString("# Billy Chat\n\n")
	b.WriteString(fmt.Sprintf("*Exported: %s*\n", time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("*Session: %s*\n\n", sessionID))
	b.WriteString("---\n\n")

	for _, msg := range messages {
		switch {
		case strings.HasPrefix(msg, "[You] "):
			b.WriteString("**You**\n\n")
			b.WriteString(strings.TrimPrefix(msg, "[You] "))
			b.WriteString("\n\n---\n\n")
		case strings.HasPrefix(msg, "[Billy] "):
			b.WriteString("**Billy**\n\n")
			b.WriteString(strings.TrimPrefix(msg, "[Billy] "))
			b.WriteString("\n\n---\n\n")
		}
	}

	return path, os.WriteFile(path, []byte(b.String()), 0644)
}

func buildMarkdown(messages []string, sessionID string) string {
	var b strings.Builder
	b.WriteString("# Billy Chat Debug\n")
	b.WriteString(fmt.Sprintf("Saved: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("Session: %s\n\n", sessionID))

	for _, msg := range messages {
		switch {
		case strings.HasPrefix(msg, "[You] "):
			b.WriteString("**You:** " + strings.TrimPrefix(msg, "[You] ") + "\n\n")
		case strings.HasPrefix(msg, "[Billy] "):
			b.WriteString("**Billy:** " + strings.TrimPrefix(msg, "[Billy] ") + "\n\n")
		default:
			b.WriteString("*" + msg + "*\n\n")
		}
	}
	return b.String()
}
