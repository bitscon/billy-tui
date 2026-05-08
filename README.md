# Billy TUI

## Purpose
Terminal UI client for Billy — a Jarvis-style command interface built with Go and the Charm stack (Bubble Tea, Lipgloss, Glamour).

## How to Run
make build
./bin/billy-tui

# Point at a non-default billy-runtime address:
./bin/billy-tui --addr http://localhost:5001
# or
BILLY_ADDR=http://localhost:5001 ./bin/billy-tui

## Structure
main.go       — entry point, flag parsing, tea.Program setup
model.go      — model struct and message types
update.go     — Elm Update: all event handlers
view.go       — Elm View: layout rendering and Markdown helper
styles.go     — Lipgloss color palette and style definitions
client.go     — HTTP client for billy-runtime /ask, /health, /runtime/status, /telemetry/events
sidebar.go    — sidebar state struct and renderSidebar() function
Makefile      — build, run, test targets
bin/          — compiled binary output (gitignored)
