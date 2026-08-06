# CHANGE: Hygiene sweep — deps, dead code, dedup, safe truncate, vet gate (Phase 4)

Date: 2026-08-05
Type: chore
ADR: none (dependency metadata, dead-code removal, refactor, and a build-target
addition — no behavior or architectural change, AGENT_OS.md §7)

## What changed

- `go.mod` / `go.sum` — ran `go mod tidy`. The four actually-imported deps
  (`bubbletea`, `bubbles`, `glamour`, `lipgloss`) are now in their own `require`
  block without the wrong `// indirect` marker; the transitive deps stay marked
  indirect. `go.sum` gained the test-dependency hashes tidy resolves for those
  modules (hash entries only — no new direct requires).
- `styles.go` — **dead code (false premise):** deleted the
  `var ( _ = colorBackground; _ = colorDiffRemove; _ = colorDim )` suppression
  block. Go never flags unused package-level vars, so the block did nothing.
  `colorBackground` is genuinely used (HintBarStyle), so it stays; `colorDim` and
  `colorDiffRemove` were used **only** by that block, so both the vars and the
  block are gone.
- `update.go` — **dedup clear-chat:** new `clearChat()` method holds the
  five-line reset (raw + display message logs, live message, stream buffer, then
  `updateChatViewport()`). The `:clear` command and the `ctrl+l` shortcut both
  call it instead of repeating the block.
- `sidebar.go` — **rune-aware truncate:** `truncate()` now converts to `[]rune`
  before measuring and slicing, so it never splits a multi-byte rune in a model
  name or denial code. Contract is unchanged (≤ max display runes, ellipsis on
  cut).
- `update.go` / `view.go` — **dedup session-id truncation:** the status-bar
  abbreviation (`view.go`, was a 16-byte slice + ellipsis) and the `:session new`
  status message (`update.go`, was a 20-byte slice, no ellipsis) now both call the
  shared rune-aware `truncate()`. Three ad-hoc truncations collapse to one helper.
- `Makefile` — **lint gate:** added a `vet` target (`go vet ./...`) and made
  `test` depend on it, so `make test` now vets before it runs the suite.

## Why

Phase 4 of the billy-tui code-review remediation — the low-severity hygiene batch,
done in one pass so a lint gate keeps the tree tidy going forward. The `// indirect`
markers misrepresented the real dependency graph; the suppression block was a no-op
built on a wrong assumption about Go; clear-chat and session-id truncation were
copy-pasted; byte-based truncation could corrupt multi-byte output; and the Makefile
never ran `go vet`.

## Risk

LOW — no runtime request payloads, the send/streaming/409/history/model-switch
paths, or governance display are touched. `clearChat()` is a verbatim extraction of
the existing reset. The rune-aware `truncate()` keeps the same `≤ max` contract and
only changes behavior for non-ASCII input near the cut point (where the old code
could split a rune). Routing the two session-id truncations through it makes the
`:session new` status string slightly shorter and gains an ellipsis — a transient
status message, not persisted or parsed. Dead-code and dependency-metadata changes
are inert. Build, vet, gofmt, and the full suite pass.

## Verified

- [x] `go build ./...` passes
- [x] `go vet ./...` passes
- [x] `make test` passes (now vets first, then runs the full suite)
- [x] `gofmt -l .` clean
- [x] `go.mod` lists bubbletea/bubbles/glamour/lipgloss as direct (no `// indirect`)
- [x] `grep -n colorDim styles.go` / `colorDiffRemove` return nothing; the
      suppression block is gone
- [x] `clearChat()` is the only place the reset lives; `:clear` and `ctrl+l` both
      call it
- [x] `truncate()` measures/slices by rune
- [x] No unrelated files changed
