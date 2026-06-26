# CHANGE: Remove fabricated "[Billy]" startup greeting

Date: 2026-06-26
Type: fix
ADR: ADR-0125

## What changed

- `model.go`: `initialModel` no longer seeds the transcript with a fabricated
  `"[Billy] Hey. What can I build for you?"` line in either `messages` or
  `displayMessages`. `messages` now starts empty so persisted transcripts
  (export / debug-save) contain only real conversation. `displayMessages` now
  starts with a single dim, non-attributed UI hint —
  `DimStyle.Render("— Say hi to start. —")` — that is clearly the interface,
  not Billy (no `[Billy]` prefix, styled distinctly from `BillyResponseStyle`).
- `update.go`: when the first user message is sent (`len(m.messages) == 0`),
  `displayMessages` is reset to `nil` before appending, so the startup hint
  disappears once the real conversation begins and never lingers above the
  transcript.

## Why

On startup the TUI displayed a fabricated Billy line before the runtime was ever
contacted — `initialModel` hardcoded `"[Billy] Hey. What can I build for you?"`.
Billy never said it; it was stale, build-framed, and made the interface feel
preloaded/scripted, and it put words in Billy's mouth that the runtime did not
produce. (The fake line was display-only: `client.go requestAsk` sends only
`{prompt, session_id}`, so it was never sent to the runtime and did not pollute
context.) The runtime already greets and asks for identity verification on first
contact, so the first `[Billy]` line in the transcript is now Billy's real
response. The blank-start feel is covered by a neutral, non-attributed hint plus
the existing input placeholder ("Message Billy…").

## Risk

LOW — change is confined to startup transcript seeding and clearing the hint on
first send. No words are attributed to Billy before the runtime responds. The
send flow, 409 handling, streaming, history, export, and debug-save paths are
unchanged (export/debug-save read `messages`, which now simply starts empty).
Rendering of an empty/seed transcript is safe (`strings.Join` on the slice).

## Verified

- [x] Tests pass
- [x] `go test ./...` passes with no errors
- [x] `go build ./...` passes with no errors
- [x] `go vet ./...` passes with no warnings
- [x] No regressions observed
- [x] Behavior matches intent
