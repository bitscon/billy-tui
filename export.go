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

// A ctrl+s chat capture has two readers at different uids: the operator who
// saves it, and the agent account that runs the "debug billy" investigation.
// Reach across those accounts comes from these modes — a 0755 directory holding
// 0644 files, the exact shape of the captures that were readable before — and
// NOT from moving the capture out of the operator's home. A world-readable
// directory under /tmp also reads across accounts, but /tmp is cleared on
// reboot: the 2026-09-08 reboot deleted every capture taken since 2026-08-11.
// Critical files do not live in a temp directory.
const (
	debugDirMode  os.FileMode = 0o755
	debugFileMode os.FileMode = 0o644
)

// saveChat writes the raw debug log — preserves all message types including
// errors and system notices. Used by ctrl+s.
//
// Both the stable "latest" pointer and the timestamped archive live under
// ~/.billy/debug (override with BILLY_DEBUG_DIR); nothing is written to the
// $HOME root. That location survives a reboot and is the path the architect
// role contract opens when the operator says "debug billy"; the modes above are
// what make the capture readable from the agent account.
// sharedDebugDir is the durable capture directory both readers share (2026-10-12): the
// operator's home is 0750, so a capture under ~/.billy/debug was unreachable from the agent
// account no matter its own mode, and "debug billy" had been reading a file last written on
// 2026-08-06. The directory is created once by the operator (owner = the operator, group dev,
// mode 2770 so every capture inherits group dev) and is never chmod-ed by this client: loosening
// it would expose captures to every account on the box.
const sharedDebugDir = "/var/lib/billy-debug"

// sharedDebugDirFor returns the shared capture directory when no override is set and the
// directory exists; shared reports whether it was chosen.
func sharedDebugDirFor(override, dir string) (string, bool) {
	if override != "" {
		return "", false
	}
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir, true
	}
	return "", false
}

func saveChat(messages []string, sessionID string) (string, error) {
	debugDir, shared := sharedDebugDirFor(os.Getenv("BILLY_DEBUG_DIR"), sharedDebugDir)
	if !shared {
		var err error
		debugDir, err = billyDir("BILLY_DEBUG_DIR", "debug")
		if err != nil {
			return "", err
		}
		// MkdirAll applies the process umask, and a directory that already exists
		// keeps the mode it was first created with, so re-assert it. Best effort: an
		// override directory owned by another account is not ours to change.
		_ = os.Chmod(debugDir, debugDirMode)
	}

	latestPath := filepath.Join(debugDir, "billy-chat-debug-latest.md")
	timestamp := time.Now().Format("2006-01-02-150405")
	archivePath := filepath.Join(debugDir, "billy-chat-"+timestamp+".md")

	content := buildMarkdown(messages, sessionID)

	if err := writeCapture(latestPath, content); err != nil {
		return "", fmt.Errorf("cannot write latest: %w", err)
	}
	if err := writeCapture(archivePath, content); err != nil {
		return "", fmt.Errorf("cannot write archive: %w", err)
	}
	return latestPath, nil
}

// writeCapture writes one capture file and pins its mode. os.WriteFile only
// applies the mode when it creates the file, so the "latest" pointer — rewritten
// on every save — would otherwise keep whatever mode it was first created with.
func writeCapture(path, content string) error {
	if err := os.WriteFile(path, []byte(content), debugFileMode); err != nil {
		return err
	}
	if err := os.Chmod(path, debugFileMode); err != nil {
		// A capture left by another account is not ours to chmod. That only
		// matters if the mode it already carries hides it from the agent
		// account — group or other read is enough, both share group dev.
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm()&0o044 == 0 {
			return fmt.Errorf("cannot make %s readable: %w", path, err)
		}
	}
	return nil
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
