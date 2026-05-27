# CHANGE: Fix double-styled message rendering in View()

Date: 2026-05-07
Type: fix
ADR: none

## What changed

- model.go: Added `displayMessages []string` field alongside `messages []string`; initialModel sets both slices with the welcome message
- update.go: All message-append sites now write plain text to `messages` and pre-styled strings to `displayMessages`; `updateChatViewport()` uses `displayMessages` directly; leading newlines from Glamour trimmed with `strings.TrimLeft` instead of `TrimRight`
- view.go: Removed per-frame re-processing loop (HasPrefix "[Billy]"/"[You]" checks and style re-application); View() builds chat content from `strings.Join(m.displayMessages, "\n")` with spinner appended if thinking

## Why

Messages stored in `m.messages` were already ANSI-styled strings in some paths, causing the prefix detection in View() to fail on styled strings and Billy responses to render incorrectly; separating plain-text storage from pre-built display strings eliminates the double-styling race.

## Risk

LOW

## Verified

- [x] Tests pass
- [x] `go build ./...` passes with no errors
- [x] `go vet ./...` passes with no warnings
- [x] No regressions observed
- [x] Behavior matches intent
