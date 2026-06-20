# PROJECT STATUS

## Current State
ADR-0125 Phase 1 complete + P5 (Step 75) complete. TUI connects to billy-runtime, renders chat with Glamour Markdown, ctrl+s saves chat to ~/billy-chat-debug-latest.md.

Operator sidebar redesigned (ADR-0125 Amendment 2026-06-20): five real-data sections — Connection+LLM, Runtime, Latency, Governance, Live Work Progress (I125) — each bound to a verified endpoint (`/runtime/status`, `/api/v1/llm/config`, `/reconciliation/recent`, `/api/v1/execution/jobs/active`) or a local measurement. Dead Observation/Memory stubs removed; prior placeholder sections that read nonexistent JSON keys are gone.

Git repository initialized for `bitscon/billy-tui`; initial scaffold commit recorded locally and `origin` configured. Push is intentionally deferred until the GitHub repository exists.

## Completed
- [Step 70] Go module scaffold — bubbletea, lipgloss, bubbles, glamour dependencies
- [Step 71] Split-pane layout — chat (70%) + sidebar (30%), input field, window resize
- [Step 72] Visual identity — Lipgloss palette, Glamour Markdown, spinner, styled status bars
- [Step 73] Live HTTP connection — POST /ask, session tracking, health check on startup
- [Step 74] Sidebar live data — telemetry polling, governance border on tool.call.denied
- [Step 75] Chat export — ctrl+s saves session to ~/billy-chat-debug-latest.md (markdown); timestamped archive in debug/; status bar confirms ✓
- [Step 82] Repository initialized — local git history created; remote origin set to git@github.com:bitscon/billy-tui.git
- [Streaming Parity Phase 3] Enter-key submit guard — blocks overlapping session submits while response is in progress
- [Sidebar Redesign] Operator sidebar rebuilt against real endpoints (ADR-0125 Amendment 2026-06-20); replaces non-functional placeholder sections

## Next Steps
- TBD (mission-progress section deferred until TUI tracks an active mission id)
