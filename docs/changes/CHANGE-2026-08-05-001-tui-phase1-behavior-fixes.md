# CHANGE: Fix three behavior bugs from the code review (Phase 1)

Date: 2026-08-05
Type: fix
ADR: none (bug fixes, not architectural changes — AGENT_OS.md §7)

## What changed

- `update.go` — **Sidebar double-poll (1a):** `Init()` started both an immediate
  one-shot `sidebarTickMsg` **and** a recurring 5s `tea.Tick`, while the
  `sidebarTickMsg` handler already reschedules itself every tick. That left two
  self-rescheduling chains running in parallel, polling the runtime ~every 2.5s
  (4 GETs each) instead of every 5s. Removed the extra `tea.Tick` from `Init`;
  the immediate one-shot now seeds the single recurring chain.
- `update.go` — **Governance notice attributed to Billy (1b):** on a governance
  rejection the tick handler appended `"[Billy] 🛡 Action blocked by governance
  policy."` to `messages`. `exportChat` and `buildMarkdown` treat any `[Billy] `
  line as Billy's words, so the export attributed this locally-generated notice
  to Billy — breaking the `model.go` invariant that the TUI must never speak AS
  Billy. The notice is now stored unprefixed (`"🛡 Action blocked by governance
  policy."`): export omits it (not conversation), the debug save renders it as a
  system note, and on-screen it is no longer falsely labeled `[Billy]`. Removed
  the now-orphaned `[Billy] 🛡` case in `rebuildDisplayMessages` (the unprefixed
  notice falls to the default error-styled branch — same red styling).
- `update.go` — **Inflated streaming token estimate (1c):** `appendStreamChunk`
  did `streamTokens += len(chunk)/4 + 1`; the per-chunk `+1` inflated the live
  count and the per-chunk integer division diverged from the non-streaming
  `len(text)/4`. The live estimate now derives from the whole buffer
  (`len(streamBuffer)/4`), and the final count at `StreamDoneMsg` is computed
  from `len(FullText)/4` — the identical basis the non-streaming path uses, so
  both report the same count for the same reply.
- `export_test.go`, `update_test.go` — **new** regression tests: export/save do
  not attribute the governance notice to Billy; streamed and non-streamed token
  estimates agree and derive from the whole text.
- `docs/changes/CHANGE-TEMPLATE.md` — **new**; the template AGENT_OS.md §7
  references did not exist in this repo (added per operator direction to fill in
  missing process on this older repo).

## Why

The code review found three issues that are actual misbehavior rather than
polish. 1a wastes runtime round-trips at double the intended rate. 1b is a
doctrine issue — the interface was putting its own words in Billy's mouth in
persisted exports, the same class of bug ADR-0125 fixed for the startup
greeting. 1c reported a token count that was both internally inconsistent
(streaming vs non-streaming) and biased high.

## Risk

LOW — all three changes are confined to the TUI's own display/estimation/polling
and touch no runtime request payloads. The send flow, 409 handling, streaming
assembly, history, and model-switch paths are unchanged. The governance notice
still shows on screen (red), just without the false attribution; the export
simply no longer includes a non-conversation status line. Build, vet, and the
full test suite (including four new tests) pass.

## Verified

- [x] `go build ./...` passes
- [x] `go vet ./...` passes
- [x] `go test ./...` passes (incl. 4 new regression tests)
- [x] `gofmt -l .` clean
- [x] Behavior matches intent
- [x] No unrelated files changed
