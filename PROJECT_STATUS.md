# PROJECT STATUS

## Current State
ADR-0125 Phase 1 complete + P5 (Step 75) complete. TUI connects to billy-runtime, renders chat with Glamour Markdown, ctrl+s saves chat under /tmp/billy/chat-log (override with BILLY_DEBUG_DIR).

Operator sidebar redesigned (ADR-0125 Amendment 2026-06-20): five real-data sections — Connection+LLM, Runtime, Latency, Governance, Live Work Progress (I125) — each bound to a verified endpoint (`/runtime/status`, `/api/v1/llm/config`, `/reconciliation/recent`, `/api/v1/execution/jobs/active`) or a local measurement. Dead Observation/Memory stubs removed; prior placeholder sections that read nonexistent JSON keys are gone.

Transport is now selectable (Modular Identity P10): `--addr` / `BILLY_ADDR` accept either `http://host:port` (TCP, the default) or `unix:///path/to/socket` (AF_UNIX). Over the socket, billy-runtime resolves the caller's principal from the kernel peer credentials, so the operator is recognised with no text challenge; TCP is unchanged and stays the default. See the README "Identity by transport" table.

Repository lives at `bitscon/billy-tui`; main is pushed and CI runs on every push.

Conductor modernization (2026-09): the client is truthful about — and now in control of — the runtime's per-turn brain routing, against `docs/CONDUCTOR-WIRE-CONTRACT.md` (v1 §1–§7 pinned 2026-09-01; v2 §8–§12 pinned 2026-09-02). Wave 1: honest routing display + per-reply `[Brain]` records + approval y/n prompt + never-lose-a-reply queue + long-stream fix. Wave 2: `:routing` mode toggle (auto|pinned, capability-gated, mode-only POST) and the `:brains` floor screen (role→minimum-brain table, one-role writes with post-write proof). Every conductor surface degrades to today's exact behavior against a runtime that predates the contract; the visible features light up when billy-runtime lands the producing side (contract v1 = runtime Modernization card 1; v2 = the floor/mode surfaces).

## Completed
- [Step 70] Go module scaffold — bubbletea, lipgloss, bubbles, glamour dependencies
- [Step 71] Split-pane layout — chat (70%) + sidebar (30%), input field, window resize
- [Step 72] Visual identity — Lipgloss palette, Glamour Markdown, spinner, styled status bars
- [Step 73] Live HTTP connection — POST /ask, session tracking, health check on startup
- [Step 74] Sidebar live data — telemetry polling, governance border on tool.call.denied
- [Step 75] Chat export — ctrl+s saves session under /tmp/billy/chat-log (latest pointer + timestamped archive, override BILLY_DEBUG_DIR; moved off ~/.billy/debug/ so the agent account can read captures); :export writes under ~/.billy/exports/ (override BILLY_EXPORT_DIR); status bar confirms ✓
- [Step 82] Repository initialized — local git history created; remote origin set to git@github.com:bitscon/billy-tui.git
- [Streaming Parity Phase 3] Enter-key submit guard — blocks overlapping session submits while response is in progress
- [Sidebar Redesign] Operator sidebar rebuilt against real endpoints (ADR-0125 Amendment 2026-06-20); replaces non-functional placeholder sections
- [Model Switching] `:model` command (ADR-0125 Amendment 2026-06-20b) — interactive picker via GET /api/v1/llm/models; `:model <provider> [model]` switches via POST /api/v1/llm/config (sanctioned operator API); preserves base_url within a provider; sidebar reflects change live
- [Modular Identity P10] Unix-socket transport — `--addr unix:///path/to/socket` dials billy-runtime over AF_UNIX for /health, /ask, /ask/stream, and the sidebar GETs; identity is transport-derived over the socket (operator recognised, no text challenge). TCP (`http://…`) preserved as the default/fallback. Proven end-to-end over a real socket (hermetic test) and against the live billy-runtime socket (opt-in `BILLY_LIVE_SOCKET` test)
- [Modernization wave 1] Conductor wire contract v1 pinned + honest routing display, per-reply brain records, approval affordance with y/n quick keys and the enter-while-busy queue, debug captures with routing detail, long-reply stream fix (Modernization 0/2/3/4/7/8; merged `aae7387`)
- [Modernization wave 2] Contract v2 pinned (floor table + mode set) + `:routing` toggle/picker and the `:brains` role→floor screen, both with legacy read-only degradation and full test coverage (Modernization 5/6 client halves; commits `5e60f51`, `187329e`, `031cb87`)

## Next Steps
The code-review remediation build (Phases 1–4) is closed. Two forward-looking feature ideas are now tracked on the billy-tui Kanboard board (Backlog, `status:blocked`); both carry an acceptance checklist there.

- **Sidebar mission-progress section** (`billy-tui:features:mission-progress`) — add a sixth real-data sidebar section for the active mission/goal + progress, bound to a real endpoint like the existing five (ADR-0125 Amendment 2026-06-20, no placeholders). Blocked until the runtime exposes an active mission id for the TUI to track.
- **Chat-driven model switching (Lever 1)** (`billy-tui:features:chat-model-switch`) — let the operator switch models conversationally, routed through the same sanctioned `SetLLMConfig` path (`POST /api/v1/llm/config`) the `:model` command already uses. Blocked on an operator decision to supersede the frozen conversational grammar `CONVERSATIONAL_RUNTIME_LLM_CONFIG_GRAMMAR_V1`.
