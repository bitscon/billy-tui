# CHANGE: Multi-line input and windowed model picker (Phase 2)

Date: 2026-08-05
Type: feat
ADR: none (UX behavior on the existing TUI widgets — no architectural change, AGENT_OS.md §7)

## What changed

- `model.go` — **Multi-line input (2a):** the input is a `textarea` but `enter`
  is intercepted for submit and its height was pinned to 1, so no newline could
  ever be entered. Rebound the textarea's `InsertNewline` to `ctrl+j` (reliably
  distinct from `enter` — LF vs CR) and `alt+enter`, set the textarea's own
  `Prompt` to `"> "` so the prompt prefixes every line of a multi-line entry, and
  added a `maxInputRows = 6` growth ceiling.
- `update.go` — **Input growth + layout sync:** new `desiredInputRows()` (grows
  one row per logical line, capped at `maxInputRows`, and further clamped on short
  terminals so the chat viewport keeps a 4-row floor) and `refreshInputHeight()`
  (resizes the input box and keeps the chat viewport height in lockstep). It is
  called from every content-change path — submit reset, `ctrl+u`, history up/down,
  restore-failed-prompt, the fall-through textarea update, and window resize — so
  the status row + panes + input + hint always sum to the terminal height.
- `view.go` — **Layout + picker:** pane/sidebar heights now derive from the actual
  input height (`m.height - 4 - inputRows`, floored at 4) instead of the hardcoded
  `-5`, and the input row renders the textarea directly (the `"> "` prompt moved
  into the widget). **Windowed model picker (2b):** new pure `modelPickerWindow()`
  windows the option list to the terminal height, keeps the selected index visible
  (centered where possible), and clamps to the option count; the overlay shows
  `↑ N more` / `↓ N more` affordances. An Ollama host with many models no longer
  overflows the screen or lets the selection move below the fold. Help overlay and
  input hint bar now mention `ctrl+j newline`.
- `update_test.go`, `view_test.go` — **new** regression tests: input grows per
  line and caps at `maxInputRows`; input is clamped harder on a short terminal;
  `refreshInputHeight` keeps the viewport in sync; `ctrl+j` inserts a newline
  end-to-end; the picker window keeps the selection visible across the whole list
  and never runs past the option count.

## Why

Phase 2 of the billy-tui code-review remediation. Both were real usability gaps:
the "multi-line" input could not go multi-line at all, and the model picker
rendered every option in one block with no windowing, so a runtime offering many
models pushed options (and the selection) off-screen.

## Risk

LOW — changes are confined to the TUI's own input widget and the model-picker
overlay. No runtime request payloads, the send/streaming/409/history/model-switch
paths, or governance display are touched. The layout arithmetic is centralized so
the input and panes always reconcile to the terminal height; rendering was
verified visually (a 3-line prompt and a 40-option picker at height 24 both fit
exactly). Build, vet, gofmt, and the full test suite (incl. new tests) pass.

## Verified

- [x] `go build ./...` passes
- [x] `go vet ./...` passes
- [x] `go test ./...` passes (incl. new Phase 2 regression tests)
- [x] `gofmt -l .` clean
- [x] Rendered `View()` inspected: multi-line input grows with consistent `> `
      prompts and the layout sums to the terminal height; picker windows to the
      screen with `↑/↓ N more` affordances and the selection centered
- [x] No unrelated files changed
