# CHANGE: Guard overlapping TUI submits

Date: 2026-05-19
Type: fix
ADR: ADR-0125

## What changed

- `update.go`: added an enter-key guard that blocks new submissions while a response is already streaming or thinking.
- `update.go`: surfaces a short status-bar message when submit is blocked so the operator knows why the turn was ignored.

## Why

Rapid repeated enter presses could issue overlapping requests on the same session and create out-of-order or conflicting responses.

## Risk

LOW

## Verified

- [x] Tests pass
- [x] `go test ./...` passes with no errors
- [x] `go build ./...` passes with no errors
- [x] `go vet ./...` passes with no warnings
- [x] No regressions observed
- [x] Behavior matches intent
