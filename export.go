package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveChat writes the raw debug log — preserves all message types including
// errors and system notices. Used by ctrl+s.
func saveChat(messages []string, sessionID string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home dir: %w", err)
	}

	latestPath := filepath.Join(homeDir, "billy-chat-debug-latest.md")

	debugDir := filepath.Join("/home/billyb/workspaces/billy-tui", "debug")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create debug dir: %w", err)
	}
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

// exportChat writes a clean, human-readable conversation export. Used by :export.
func exportChat(messages []string, sessionID string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home dir: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02-150405")
	filename := fmt.Sprintf("billy-chat-%s.md", timestamp)
	path := filepath.Join(homeDir, filename)

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
