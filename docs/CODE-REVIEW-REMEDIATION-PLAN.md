# billy-tui — code-review remediation plan

Parked 2026-08-05. This is the pickup point for acting on the code review of
billy-tui. The review found no blockers — the app builds, vets, and tests clean —
but there are three real behavior issues, two UX gaps, and a batch of hygiene.

When we pick this up, it runs as **one build ("review fixes") on the billy-tui
Kanboard board**, four phases, one phase per session, under the Kanboard-native
phase model (AGENT_OS.md Section 19). Each phase below becomes a card.

Baseline at time of review: `go build`, `go vet`, `go test ./...` all green.

---

## Phase 1 — Fix the three real behavior issues

### 1a. Sidebar polls Billy twice as often as intended
`Init()` (update.go:227) starts both a one-shot immediate refresh **and** a
recurring 5s timer, and the `sidebarTickMsg` handler reschedules itself every
tick (update.go:504). Two self-rescheduling chains run in parallel → the runtime
is polled ~every 2.5s, and each poll is 4 GETs. Fix: one recurring source only —
make the immediate refresh a one-shot that doesn't reschedule, or drop
`sidebarTick` from `Init`.

### 1b. A TUI status line is exported as if Billy said it
On a governance rejection, update.go:459 appends
`"[Billy] 🛡 Action blocked by governance policy."` to `m.messages`; `exportChat`
(export.go:67) treats any `[Billy] ` line as Billy's words, so the export
attributes a locally-generated notice to Billy — breaking the invariant at
model.go:168 ("The TUI must never speak AS Billy"). Fix: give TUI notices their
own marker, or filter the shield line out of `exportChat`/`buildMarkdown`.

### 1c. Streaming token estimate is inflated
`appendStreamChunk` (update.go:35) does `streamTokens += len(chunk)/4 + 1` — the
`+1` per chunk balloons the count and diverges from the non-streaming
`len(text)/4` (update.go:349). Fix: at `StreamDoneMsg`, compute
`len(streamBuffer)/4` so both paths agree.

## Phase 2 — Close the two UX gaps

### 2a. Multi-line input widget that can't go multi-line
Input is a `textarea` (model.go:153) but `enter` is intercepted for submit
(update.go:624) before the widget sees it, and height is pinned to 1
(model.go:158). No newline can be entered, so multi-line prompts are impossible.
Fix: add a newline key (shift+enter / alt+enter / ctrl+j) and let it grow, or
drop to `textinput` and be honest about it.

### 2b. Model picker overflows on long lists
`modelPickerMode` (view.go:154) renders every option in one centered block with
no windowing; the selection index (update.go:515) can move below the fold. An
Ollama host with many models makes it unusable. Fix: window the list around
`modelPickerIdx` to the available height with a "+N more" affordance.

## Phase 3 — Remaining UX polish

### 3a. Mouse capture disables native text selection
`tea.WithMouseCellMotion()` (main.go:24) takes over the mouse, so click-drag
select-and-copy stops working — you can't mouse-copy Billy's replies. Fix:
consider dropping mouse capture (keyboard scroll already covers it) or gate it
behind a toggle.

### 3b. Two clutter piles in `$HOME` root
`:export` writes `~/billy-chat-<date>.md` (export.go:53) and `ctrl+s` writes
`~/billy-chat-debug-latest.md` (export.go:19) into the home root, while the
ctrl+s **archive** correctly uses `~/.billy/debug` with a `BILLY_DEBUG_DIR`
override. Fix: default both to `~/.billy/…` and honor an env override for export.

## Phase 4 — Hygiene sweep

- **go.mod marks direct deps as `// indirect`** (bubbletea, bubbles, glamour,
  lipgloss). Run `go mod tidy`.
- **Dead suppression block on a false premise** (styles.go:123): Go never flags
  unused package-level vars, so `var (_ = colorBackground; …)` does nothing, and
  `colorBackground` is actually used. Delete the block; delete or wire up the
  genuinely-unused `colorDim` and `colorDiffRemove`.
- **Duplicated logic**: clear-chat exists twice (`:clear` update.go:178 and
  `ctrl+l` update.go:591); session-id truncation appears three times with three
  limits (view.go:84, update.go:197, sidebar `truncate`). Fold into helpers.
- **`truncate()` is byte-based** (sidebar.go:173) — can split a multi-byte rune
  in a model name / denial code. Make it rune-aware.
- **No `go vet` / lint gate** — Makefile `test` runs only `go test`. Add
  `go vet ./...` (optionally `golangci-lint`).

---

*Full original review with the same file:line refs is preserved in this plan.
Suggested order is top to bottom; Phase 1 is the highest value and smallest.*
