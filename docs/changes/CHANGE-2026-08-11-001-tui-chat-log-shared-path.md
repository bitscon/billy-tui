# CHANGE: Save the chat debug log to a shared path the agent account can read

Date: 2026-08-11
Type: fix
ADR: none

## What changed

- `export.go`: `saveChat` (ctrl+s) now defaults its output directory to
  `/tmp/billy/chat-log` instead of `~/.billy/debug`. Added the `defaultDebugDir`
  constant and resolved the dir directly (env `BILLY_DEBUG_DIR` override still
  honored) rather than through the `$HOME`-based `billyDir` helper. `exportChat`
  (`:export`) is unchanged — it still lands under `~/.billy/exports`.
- `export_test.go`: retargeted the save test to assert the debug dir via the
  `BILLY_DEBUG_DIR` override (hermetic), and added `TestDefaultDebugDirIsOutsideHome`
  pinning the default to `/tmp/billy/chat-log` and forbidding a `/home/` default.

## Why

The "debug billy" flow runs under the `claude` agent account, but billy-tui runs
under the operator account `billybs`. Since the 3-account split (2026-08-07),
ctrl+s captures landed in `/home/billybs/.billy/debug` (`0750`, owned `billybs`),
which the agent account cannot traverse — so the architect could not read the very
log it is contracted (`agents/architect/AGENTS.md` §12) to open. Root cause is
cross-account reachability, not the log itself: the writer and the reader are
different uids, and a `$HOME`-scoped path is unreachable across them.

`/tmp/billy/chat-log` is world-readable, needs no root setup, and is created by
billy-tui itself on first save. All three barn principals are in group `dev`, but
reachability here rides on the `0755`/`0644` world bits, not the group. The
`BILLY_DEBUG_DIR` override lets us promote to a reboot-durable
`/var/lib/billy/chat-log` later without a recompile.

## Risk

LOW — a default-path constant plus a rebuild in the client TUI. No service,
systemd, or runtime change. Blast radius is where ctrl+s writes; the override
preserves the old behavior for anyone who sets `BILLY_DEBUG_DIR`. One residual
sharp edge: if a *different* account creates `/tmp/billy` first at `0755`,
billy-tui (as `billybs`) cannot write into it — mitigated because billy-tui is the
sole writer and the dir is left for it to create.

## Verified

- [x] `go build ./...` passes
- [x] `go vet ./...` passes
- [x] `go test ./...` passes
- [x] Behavior matches intent (default resolves to `/tmp/billy/chat-log`; override honored; `/tmp/billy` left absent so billy-tui creates it as `billybs`)
- [x] No unrelated files changed (pre-existing `Makefile` edit left untouched)
