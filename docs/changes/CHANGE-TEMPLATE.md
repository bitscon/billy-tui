# CHANGE: <short imperative title>

Date: YYYY-MM-DD
Type: <feat | fix | refactor | test | docs | chore | governance>
ADR: <ADR-XXXX in billy-runtime/docs/adr/, or "none">

## What changed

- <file>: <what changed and why, one bullet per distinct change>

## Why

<The problem this solves and the reasoning behind the approach. State the root
cause, not just the symptom.>

## Risk

<LOW | MEDIUM | HIGH> — <one line: what could break, and what bounds the blast
radius.>

## Verified

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] Behavior matches intent
- [ ] No unrelated files changed
