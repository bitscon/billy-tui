# CHANGE: offer Billy's pending questions as a picker, and add :permissions

Date: 2026-10-10
Type: feat
ADR: ADR-0258 (billy-runtime/docs/adr/ADR-0258-ask-before-the-cloud-reads.md)

## What changed

- docs/CONDUCTOR-WIRE-CONTRACT.md: v3 pins the runtime's pending_question object, its kinds and the picker rules.
- client.go / model.go: decode pending_question on the ask reply and the stream's final frame; absent or malformed means nothing pending.
- update.go / view.go: a picker in the model picker's visual language (arrows or j/k, Enter, number keys 1-4, Esc sends nothing); the answer goes out on the session the turn ran on, as the option number (or the exact label for the kinds the runtime matches by label); unsafe labels are refused, never echoed; :permissions sends /permissions as the whole message.
- client_test.go / update_test.go / view_test.go: decode, picker, session, :permissions and degradation pins.

## Why

The operator ruled (2026-10-10) that Billy asks before a cloud brain reads his board, his saved reading, his files or Billy's memory, as choices rather than typed sentences. The runtime half is sealed (billy-runtime STEP-710); this client showed the choices only as a numbered list.

## Risk

LOW: additive; an older runtime that sends no pending_question leaves the client exactly as before.

## Verified

- [x] go build ./... passes
- [x] go vet ./... passes
- [x] go test ./... passes (fresh, -count=1)
- [x] Behavior matches intent (eight self-review waves; stopped by the architect at the layout-review cap the operator set)
- [x] No unrelated files changed
