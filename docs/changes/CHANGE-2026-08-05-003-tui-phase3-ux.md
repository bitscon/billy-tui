# CHANGE: Mouse text-selection toggle and tidy save/export locations (Phase 3)

Date: 2026-08-05
Type: feat
ADR: none (UX behavior + output-path defaults on the existing TUI — no architectural change, AGENT_OS.md §7)

## What changed

- `main.go` — **Mouse text-selection (3a):** dropped `tea.WithMouseCellMotion()`
  from the program options so the TUI starts with mouse capture OFF. The terminal's
  native click-drag select-and-copy now works out of the box, so Billy's replies can
  be mouse-copied. Nothing in the app consumed mouse events by default and keyboard
  scroll (pgup/pgdn, tab-to-chat + arrows) already covers navigation.
- `model.go` — new `mouseCapture bool` field tracking capture state (starts false).
- `update.go` — **Mouse toggle (3a):** new `ctrl+t` global shortcut flips mouse
  capture at runtime, returning `tea.EnableMouseCellMotion` (on → viewport gains
  mouse-wheel scroll, native selection suppressed) or `tea.DisableMouse` (off →
  native select-and-copy restored), with a status-bar line confirming the new state.
- `export.go` — **Tidy $HOME (3b):** new `billyDir(env, sub...)` helper resolves an
  output directory (env override, else `~/.billy/<sub>`) and creates it. `saveChat`
  (ctrl+s) now writes **both** the `billy-chat-debug-latest.md` pointer and the
  timestamped archive under `~/.billy/debug` (override `BILLY_DEBUG_DIR`) — the
  "latest" copy previously landed in the `$HOME` root. `exportChat` (:export) now
  writes under `~/.billy/exports` (override `BILLY_EXPORT_DIR`) instead of the
  `$HOME` root. No more `billy-chat-*.md` files dumped into home.
- `view.go` — help overlay adds the `ctrl+t` toggle line and points `:export` at
  `~/.billy/exports/`.
- `PROJECT_STATUS.md` — updated the two references to the old `~/billy-chat-debug-latest.md`
  home-root path to the new `~/.billy/debug/` + `~/.billy/exports/` locations (state
  consistency, AGENT_OS.md §9).
- `export_test.go`, `update_test.go` — **new** regression tests: `:export` defaults
  under `~/.billy/exports` and honors `BILLY_EXPORT_DIR`; ctrl+s save puts the latest
  pointer and archive under `~/.billy/debug` with nothing in the `$HOME` root; `ctrl+t`
  flips `mouseCapture` and emits a mouse command each way.

## Why

Phase 3 of the billy-tui code-review remediation, the two remaining UX-polish items:
(3a) unconditional mouse capture disabled the terminal's own text selection, so you
could not click-drag to copy Billy's replies; (3b) `:export` and the ctrl+s "latest"
copy dumped `billy-chat-*.md` files into the `$HOME` root while the ctrl+s archive was
already correctly tucked under `~/.billy/debug`. Default capture-off fixes selection
for everyone while `ctrl+t` keeps mouse-wheel scroll available on demand; routing all
artifacts under `~/.billy` (with env overrides) keeps the home root clean.

## Risk

LOW — changes are confined to program startup options, one new keybinding, and the
two file-writing helpers. No runtime request payloads, the send/streaming/409/history
/model-switch paths, or governance display are touched. Save/export return the path
they wrote, so the status-bar confirmation still shows the real location. Both env
overrides are honored and covered by tests. Build, vet, gofmt, and the full suite
(incl. new tests) pass.

## Verified

- [x] `go build ./...` passes
- [x] `go vet ./...` passes
- [x] `go test ./...` passes (incl. new Phase 3 regression tests)
- [x] `gofmt -l .` clean
- [x] `ctrl+t` toggle flips `mouseCapture` and returns the enable/disable mouse
      command each way (unit test); startup no longer requests mouse capture, so
      native text-selection is restored by default
- [x] `:export` and ctrl+s write under `~/.billy/…`, honor `BILLY_EXPORT_DIR` /
      `BILLY_DEBUG_DIR`, and leave the `$HOME` root clean (unit tests)
- [x] No unrelated files changed
