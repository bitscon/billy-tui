# Billy TUI

## Purpose
Terminal UI client for Billy — a Jarvis-style command interface built with Go and the Charm stack (Bubble Tea, Lipgloss, Glamour).

## How to Run
make build
./bin/billy-tui

### Transport: TCP (default) or Unix socket
The `--addr` flag (or `BILLY_ADDR`) accepts two address forms; nothing else about
the session differs — same endpoints, payloads, streaming, and UI.

    # TCP over loopback (the default):
    ./bin/billy-tui --addr http://localhost:5001
    BILLY_ADDR=http://localhost:5001 ./bin/billy-tui

    # Unix domain socket (local barn):
    ./bin/billy-tui --addr unix:///home/billyb/.billy/sock/billy.sock
    BILLY_ADDR=unix:///home/billyb/.billy/sock/billy.sock ./bin/billy-tui

    # Remote laptop -> barn, over an SSH-forwarded socket:
    ssh -N -L /tmp/billy.sock:/home/billyb/.billy/sock/billy.sock barn &
    ./bin/billy-tui --addr unix:///tmp/billy.sock
    # (see billy-runtime/docs/runbooks/RUNBOOK-billy-remote-access.md)

### Identity by transport
Billy resolves who is connecting from the transport itself — no text challenge —
when the connection carries kernel peer credentials:

| Connection | Principal Billy sees |
|---|---|
| Unix socket, local barn | the connecting OS user (operator = uid 1000 -> recognised as operator) |
| Unix socket, SSH-forwarded | the SSH-login user on the barn (log in as the operator -> operator) |
| TCP (`http://…`) | no peer credentials — Billy falls back to asking who you are |

Run the TUI as the operator's account over the socket and Billy greets you with no
identity question. The client does nothing special for identity; the socket speaks.

## Structure
main.go       — entry point, flag parsing, tea.Program setup
model.go      — model struct and message types
update.go     — Elm Update: all event handlers
view.go       — Elm View: layout rendering and Markdown helper
styles.go     — Lipgloss color palette and style definitions
client.go     — HTTP client for billy-runtime: /health, /ask, /ask/stream, /runtime/status, /reconciliation/recent, /api/v1/llm/config, /api/v1/llm/models, /api/v1/execution/jobs/active
sidebar.go    — sidebar state struct and renderSidebar() function
Makefile      — build, run, test targets
bin/          — compiled binary output (gitignored)
