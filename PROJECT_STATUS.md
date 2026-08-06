# PROJECT STATUS

## Current State
ADR-0125 Phase 1 complete + P5 (Step 75) complete. TUI connects to billy-runtime, renders chat with Glamour Markdown, ctrl+s saves chat under ~/.billy/debug/.

Operator sidebar redesigned (ADR-0125 Amendment 2026-06-20): five real-data sections — Connection+LLM, Runtime, Latency, Governance, Live Work Progress (I125) — each bound to a verified endpoint (`/runtime/status`, `/api/v1/llm/config`, `/reconciliation/recent`, `/api/v1/execution/jobs/active`) or a local measurement. Dead Observation/Memory stubs removed; prior placeholder sections that read nonexistent JSON keys are gone.

Git repository initialized for `bitscon/billy-tui`; initial scaffold commit recorded locally and `origin` configured. Push is intentionally deferred until the GitHub repository exists.

## Completed
- [Step 70] Go module scaffold — bubbletea, lipgloss, bubbles, glamour dependencies
- [Step 71] Split-pane layout — chat (70%) + sidebar (30%), input field, window resize
- [Step 72] Visual identity — Lipgloss palette, Glamour Markdown, spinner, styled status bars
- [Step 73] Live HTTP connection — POST /ask, session tracking, health check on startup
- [Step 74] Sidebar live data — telemetry polling, governance border on tool.call.denied
- [Step 75] Chat export — ctrl+s saves session under ~/.billy/debug/ (latest pointer + timestamped archive, override BILLY_DEBUG_DIR); :export writes under ~/.billy/exports/ (override BILLY_EXPORT_DIR); status bar confirms ✓
- [Step 82] Repository initialized — local git history created; remote origin set to git@github.com:bitscon/billy-tui.git
- [Streaming Parity Phase 3] Enter-key submit guard — blocks overlapping session submits while response is in progress
- [Sidebar Redesign] Operator sidebar rebuilt against real endpoints (ADR-0125 Amendment 2026-06-20); replaces non-functional placeholder sections
- [Model Switching] `:model` command (ADR-0125 Amendment 2026-06-20b) — interactive picker via GET /api/v1/llm/models; `:model <provider> [model]` switches via POST /api/v1/llm/config (sanctioned operator API); preserves base_url within a provider; sidebar reflects change live

## Next Steps
The code-review remediation build (Phases 1–4) is closed. Two forward-looking feature ideas are now tracked on the billy-tui Kanboard board (Backlog, `status:blocked`); both carry an acceptance checklist there.

- **Sidebar mission-progress section** (`billy-tui:features:mission-progress`) — add a sixth real-data sidebar section for the active mission/goal + progress, bound to a real endpoint like the existing five (ADR-0125 Amendment 2026-06-20, no placeholders). Blocked until the runtime exposes an active mission id for the TUI to track.
- **Chat-driven model switching (Lever 1)** (`billy-tui:features:chat-model-switch`) — let the operator switch models conversationally, routed through the same sanctioned `SetLLMConfig` path (`POST /api/v1/llm/config`) the `:model` command already uses. Blocked on an operator decision to supersede the frozen conversational grammar `CONVERSATIONAL_RUNTIME_LLM_CONFIG_GRAMMAR_V1`.
