# CHANGE: Graceful handling of HTTP 409 session_turn_in_progress

Date: 2026-06-26
Type: fix
ADR: ADR-0125

## What changed

- `client.go`: added `errTurnInProgress` sentinel, `turnInProgressMessage`
  constant, and `is409TurnInProgress(resp)` helper that classifies a non-200
  response as the runtime's one-turn-at-a-time guard (HTTP 409 with body code
  `session_turn_in_progress`, or a bare/non-JSON 409 on the single-turn `/ask`
  endpoints). A 409 carrying a *different* code is NOT classified as the guard.
- `client.go`: `requestAsk` (`/ask`) and `openAskStream` (`/ask/stream`) now
  return `errTurnInProgress` on a 409 turn-in-progress instead of the raw
  `"ask request failed: 409"` / `"stream request failed: 409"`.
- `client.go`: the `ask` command maps `errTurnInProgress` to a new
  `turnInProgressMsg` instead of `errMsg`.
- `model.go`: added `turnInProgressMsg` message type.
- `update.go`: added `handleTurnInProgress()` which clears in-flight state and
  shows a transient, non-alarming status line
  (`⏳ Billy is still thinking — give him a moment…`). Handles the new
  `turnInProgressMsg`, and detects `errTurnInProgress` inside the `StreamErrMsg`
  branch so the stream path does NOT fall back to a non-streaming `/ask` retry
  (which would 409 again).
- `client_test.go` (new): unit tests for `is409TurnInProgress`, and for the
  `/ask` + `/ask/stream` 409 mapping via httptest, plus a test asserting non-409
  errors keep their original message.

## Why

The runtime correctly rejects a second request while a turn is in progress with
HTTP 409 / `session_turn_in_progress`. The TUI treated every non-200 as a hard
error, so the operator saw the alarming `⚠️  ask request failed: 409`. With the
slow live model (qwen3.5:9b) this happens often. The fix surfaces a friendly,
transient state instead, and avoids a pointless non-streaming retry that would
just 409 again.

## Risk

LOW — change is confined to error classification on the `/ask` and `/ask/stream`
paths. The success path and all other non-200 handling are unchanged; a 409 with
a non-matching code still falls through to the generic error. No new retries are
introduced (the guard is surfaced, not hammered). The operator's text remains
protected by the existing submit guard, so they can simply resend.

## Verified

- [x] Tests pass
- [x] `go test ./...` passes with no errors
- [x] `go build ./...` passes with no errors
- [x] `go vet ./...` passes with no warnings
- [x] No regressions observed
- [x] Behavior matches intent
